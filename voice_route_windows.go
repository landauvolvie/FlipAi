//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// The desktop AI app must use the cables, not the PC's real microphone and
// speakers -- and nobody should have to open the app's audio settings to make
// that true. Windows keeps a per-application default-device store (the same
// one Settings > App volume and device preferences writes), reachable through
// the AudioPolicyConfig factory; FlipAi writes the app's entries there itself,
// pointing its playback at the return cable and its recording at the caller
// cable. The app picks the assignment up when it next opens an audio stream,
// which is exactly when its voice mode starts.
//
// The store is keyed by the process, so the app has to be running to be
// routed. Routing is applied whenever the device list changes and again while
// the phone is still ringing, before anything opens an audio stream -- see
// startAgentVoiceSession, which is the only caller that matters. The outcome,
// including "the app is not running yet", is recorded where the Settings page
// shows it.

var (
	procVoiceGetWindowThreadProcessID = voiceUser32.NewProc("GetWindowThreadProcessId")
)

func windowProcessID(hwnd uintptr) uint32 {
	var pid uint32
	procVoiceGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

// voiceRouteMu keeps concurrent routing attempts (a device report racing a
// call) from interleaving their PowerShell runs and their notes.
var voiceRouteMu sync.Mutex

// routeAgentAppAudio points one agent's desktop app at the cables. Best
// effort: every outcome is written to RoutingNote so the status page can say
// what actually happened.
func routeAgentAppAudio(dataDir string, cfg VoiceCallConfig, agent string) {
	voiceRouteMu.Lock()
	defer voiceRouteMu.Unlock()
	note := func(text string) {
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) { s.RoutingNote = text })
	}
	target := voiceAgentConfig(cfg, agent)
	plan := currentVoiceCablePlan(dataDir)
	if plan.AgentInput == "" && plan.AgentOutput == "" {
		note("Not applied: " + plan.Warning)
		return
	}
	hwnd := findAgentAppWindow(agent, target)
	if hwnd == 0 {
		note(fmt.Sprintf("Waiting for the %s desktop app: its audio is routed to the cables the moment its window exists.", target.AppTitle))
		return
	}
	pid := windowProcessID(hwnd)
	if pid == 0 {
		note(fmt.Sprintf("Could not identify the %s desktop app's process, so its audio was not re-routed.", target.AppTitle))
		return
	}
	if err := setAppDefaultEndpoints(dataDir, pid, plan.AgentOutput, plan.AgentInput); err != nil {
		note(fmt.Sprintf("Windows refused the automatic audio routing for %s: %v. One-time fallback: in the app's own audio settings choose %q as its microphone and %q as its speaker.", target.AppTitle, err, plan.AgentInput, plan.AgentOutput))
		return
	}
	note(fmt.Sprintf("%s is wired to the cables: it hears the caller on %q and speaks into %q. Applied automatically; nothing to choose in the app.", target.AppTitle, plan.AgentInput, plan.AgentOutput))
}

// platformVoiceDevicesChanged runs when the Google Voice page reports a fresh
// device list: the wiring may just have become possible (a cable installed,
// the app started), so the enabled agents are re-routed in the background.
var voiceRoutePending sync.Mutex
var voiceRouteQueued bool

func platformVoiceDevicesChanged(dataDir string) {
	voiceRoutePending.Lock()
	if voiceRouteQueued {
		voiceRoutePending.Unlock()
		return
	}
	voiceRouteQueued = true
	voiceRoutePending.Unlock()
	go func() {
		defer func() {
			voiceRoutePending.Lock()
			voiceRouteQueued = false
			voiceRoutePending.Unlock()
		}()
		time.Sleep(1 * time.Second)
		cfg := loadVoiceCallConfig(dataDir)
		// The default agent's app is the one a call will reach; route it first
		// so the common case is wired before any call. The other agent is
		// routed at call time if one ever reaches it.
		routeAgentAppAudio(dataDir, cfg, cfg.DefaultAgent)
	}()
}

// setAppDefaultEndpoints writes one process's default render and capture
// endpoints into the Windows per-app audio policy store, finding the
// endpoints by their friendly names. It shells out to PowerShell because the
// store is only reachable through a WinRT factory, and the well-understood
// interop for it is C#.
func setAppDefaultEndpoints(dataDir string, pid uint32, renderLabel, captureLabel string) error {
	script := filepath.Join(dataDir, "route-app-audio.ps1")
	if err := os.WriteFile(script, []byte(routeAppAudioPS), 0600); err != nil {
		return err
	}
	args := []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", script, "-ProcessId", strconv.FormatUint(uint64(pid), 10)}
	if renderLabel != "" {
		args = append(args, "-RenderName", renderLabel)
	}
	if captureLabel != "" {
		args = append(args, "-CaptureName", captureLabel)
	}
	cmd := exec.Command("powershell.exe", args...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("%s", truncate(text, 400))
	}
	return nil
}

// routeAppAudioPS is the PowerShell/C# helper that does the actual write. The
// interop mirrors what EarTrumpet and Windows' own Settings app use: resolve
// the endpoint IDs through MMDevice, then persist them per-process through
// the AudioPolicyConfig factory (trying the 21H2+ interface first, then the
// original one).
const routeAppAudioPS = `
param(
  [Parameter(Mandatory=$true)][uint32]$ProcessId,
  [string]$RenderName = '',
  [string]$CaptureName = ''
)
$ErrorActionPreference = 'Stop'
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;

public static class FlipAudioRoute
{
    [ComImport, Guid("BCDE0395-E52F-467C-8E3D-C4579291692E")]
    private class MMDeviceEnumeratorCom { }

    [ComImport, Guid("A95664D2-9614-4F35-A746-DE8DB63617E6"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    private interface IMMDeviceEnumerator
    {
        int EnumAudioEndpoints(int dataFlow, int stateMask, out IMMDeviceCollection devices);
    }

    [ComImport, Guid("0BD7A1BE-7A1A-44DB-8397-CC5392387B5E"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    private interface IMMDeviceCollection
    {
        int GetCount(out int count);
        int Item(int index, out IMMDevice device);
    }

    [ComImport, Guid("D666063F-1587-4E43-81F1-B948E807363F"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    private interface IMMDevice
    {
        int Activate(ref Guid iid, int clsCtx, IntPtr activationParams, out IntPtr iface);
        int OpenPropertyStore(int access, out IPropertyStore properties);
        int GetId(out IntPtr id);
        int GetState(out int state);
    }

    [ComImport, Guid("886d8eeb-8cf2-4446-8d02-cdba1dbdcf99"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    private interface IPropertyStore
    {
        int GetCount(out int count);
        int GetAt(int index, out PropertyKey key);
        int GetValue(ref PropertyKey key, out PropVariant value);
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct PropertyKey { public Guid fmtid; public int pid; }

    [StructLayout(LayoutKind.Explicit)]
    private struct PropVariant { [FieldOffset(0)] public ushort vt; [FieldOffset(8)] public IntPtr p; }

    // The per-app policy store. The interface layout is stable; its IID moved
    // once, at Windows 11 21H2.
    [ComImport, Guid("ab3d4648-e242-459f-b02f-541c70306324"), InterfaceType(ComInterfaceType.InterfaceIsIInspectable)]
    private interface IAudioPolicyConfigFactory21H2
    {
        int a(); int b(); int c(); int d(); int e(); int f(); int g(); int h();
        int i(); int j(); int k(); int l(); int m(); int n(); int o(); int p();
        int q(); int r(); int s();
        [PreserveSig] int SetPersistedDefaultAudioEndpoint(uint processId, int flow, int role, [MarshalAs(UnmanagedType.HString)] string deviceId);
        [PreserveSig] int GetPersistedDefaultAudioEndpoint(uint processId, int flow, int role, [MarshalAs(UnmanagedType.HString)] out string deviceId);
        [PreserveSig] int ClearAllPersistedApplicationDefaultEndpoints();
    }

    [ComImport, Guid("2a59116d-6c4f-45e0-a74f-707e3fef9258"), InterfaceType(ComInterfaceType.InterfaceIsIInspectable)]
    private interface IAudioPolicyConfigFactoryPre21H2
    {
        int a(); int b(); int c(); int d(); int e(); int f(); int g(); int h();
        int i(); int j(); int k(); int l(); int m(); int n(); int o(); int p();
        int q(); int r(); int s();
        [PreserveSig] int SetPersistedDefaultAudioEndpoint(uint processId, int flow, int role, [MarshalAs(UnmanagedType.HString)] string deviceId);
        [PreserveSig] int GetPersistedDefaultAudioEndpoint(uint processId, int flow, int role, [MarshalAs(UnmanagedType.HString)] out string deviceId);
        [PreserveSig] int ClearAllPersistedApplicationDefaultEndpoints();
    }

    [DllImport("combase.dll")]
    private static extern int RoInitialize(int initType);

    [DllImport("combase.dll", PreserveSig = false)]
    private static extern void RoGetActivationFactory(
        [MarshalAs(UnmanagedType.HString)] string activatableClassId,
        [In] ref Guid iid,
        [MarshalAs(UnmanagedType.IInspectable)] out object factory);

    private static string FindEndpointId(int flow, string name)
    {
        var enumerator = (IMMDeviceEnumerator)new MMDeviceEnumeratorCom();
        IMMDeviceCollection devices;
        Marshal.ThrowExceptionForHR(enumerator.EnumAudioEndpoints(flow, 1 /*ACTIVE*/, out devices));
        int count;
        Marshal.ThrowExceptionForHR(devices.GetCount(out count));
        var friendly = new PropertyKey { fmtid = new Guid("a45c254e-df1c-4efd-8020-67d146a850e0"), pid = 14 };
        string loose = null;
        for (int i = 0; i < count; i++)
        {
            IMMDevice device;
            Marshal.ThrowExceptionForHR(devices.Item(i, out device));
            IPropertyStore store;
            Marshal.ThrowExceptionForHR(device.OpenPropertyStore(0 /*STGM_READ*/, out store));
            PropVariant value;
            Marshal.ThrowExceptionForHR(store.GetValue(ref friendly, out value));
            string label = value.vt == 31 ? Marshal.PtrToStringUni(value.p) : null;
            if (string.IsNullOrEmpty(label)) continue;
            IntPtr idPtr;
            Marshal.ThrowExceptionForHR(device.GetId(out idPtr));
            string id = Marshal.PtrToStringUni(idPtr);
            Marshal.FreeCoTaskMem(idPtr);
            if (string.Equals(label, name, StringComparison.OrdinalIgnoreCase)) return id;
            if (loose == null && label.IndexOf(name, StringComparison.OrdinalIgnoreCase) >= 0) loose = id;
        }
        if (loose != null) return loose;
        throw new Exception("no active " + (flow == 0 ? "playback" : "recording") + " endpoint is named '" + name + "'");
    }

    private static string SwdId(int flow, string mmDeviceId)
    {
        var iface = flow == 0 ? "{e6327cad-dcec-4949-ae8a-991e976a79d2}" : "{2eef81be-33fa-4800-9670-1cd474972c3f}";
        return "\\\\?\\SWD#MMDEVAPI#" + mmDeviceId + "#" + iface;
    }

    private static int Persist(object factory, uint processId, int flow, string deviceId)
    {
        var f21 = factory as IAudioPolicyConfigFactory21H2;
        int hr = 0;
        foreach (var role in new[] { 0 /*Console*/, 1 /*Multimedia*/, 2 /*Communications*/ })
        {
            if (f21 != null) hr = f21.SetPersistedDefaultAudioEndpoint(processId, flow, role, deviceId);
            else hr = ((IAudioPolicyConfigFactoryPre21H2)factory).SetPersistedDefaultAudioEndpoint(processId, flow, role, deviceId);
            if (hr != 0) return hr;
        }
        return 0;
    }

    public static void Route(uint processId, string renderName, string captureName)
    {
        object factory = null;
        try { RoInitialize(1 /*multithreaded*/); } catch { }
        var cls = "Windows.Media.Internal.AudioPolicyConfig";
        try
        {
            var iid = typeof(IAudioPolicyConfigFactory21H2).GUID;
            RoGetActivationFactory(cls, ref iid, out factory);
        }
        catch
        {
            var iid = typeof(IAudioPolicyConfigFactoryPre21H2).GUID;
            RoGetActivationFactory(cls, ref iid, out factory);
        }
        if (!string.IsNullOrEmpty(renderName))
        {
            var hr = Persist(factory, processId, 0, SwdId(0, FindEndpointId(0, renderName)));
            if (hr != 0) throw new Exception("persisting the playback endpoint failed with HRESULT 0x" + hr.ToString("X8"));
        }
        if (!string.IsNullOrEmpty(captureName))
        {
            var hr = Persist(factory, processId, 1, SwdId(1, FindEndpointId(1, captureName)));
            if (hr != 0) throw new Exception("persisting the recording endpoint failed with HRESULT 0x" + hr.ToString("X8"));
        }
    }
}
'@
try {
  [FlipAudioRoute]::Route($ProcessId, $RenderName, $CaptureName)
  Write-Output 'routed'
} catch {
  Write-Output $_.Exception.Message
  exit 1
}
`
