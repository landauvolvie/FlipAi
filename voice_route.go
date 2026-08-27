package main

// The outcomes of pointing the desktop app's audio at the cables. They are
// recorded as a state rather than inferred from the note's wording, because a
// user who is told "Waiting" when the real problem is that no cable exists goes
// looking at entirely the wrong thing.
const (
	voiceRoutingApplied       = "applied"
	voiceRoutingNoCables      = "no-cables"
	voiceRoutingWaitingForApp = "waiting-for-app"
	voiceRoutingRefused       = "refused"
)

// The per-application audio routing helper lives in a platform-independent file
// so the interop it depends on -- the endpoint lookup and the persisted per-app
// default write -- can be checked by a test on any machine. Only the code that
// runs it is Windows-only.

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
