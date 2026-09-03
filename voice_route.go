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
// AudioPolicyConfig interface and HSTRING ABI mirror EarTrumpet. Electron adds
// one extra wrinkle: the PID owning the top-level ChatGPT window is not
// necessarily the PID owning its audio session. The helper therefore writes the
// policy for the whole live process tree. startAgentVoiceSessionVerified runs it
// once before Voice opens and once again after Voice is active, when Electron's
// audio utility process definitely exists.
const routeAppAudioPS = `
param(
  [Parameter(Mandatory=$true)][uint32]$ProcessId,
  [string]$RenderName = '',
  [string]$CaptureName = ''
)
$ErrorActionPreference = 'Stop'
Add-Type -TypeDefinition @'
using System;
using System.Collections.Generic;
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

    // These declarations are the same method order used by current EarTrumpet.
    // Set takes a native HSTRING handle. v0.45 corrected that ABI; v0.46 keeps
    // it and fixes which Electron process IDs the policy is persisted against.
    [ComImport, Guid("ab3d4648-e242-459f-b02f-541c70306324"), InterfaceType(ComInterfaceType.InterfaceIsIInspectable)]
    private interface IAudioPolicyConfigFactory21H2
    {
        int a(); int b(); int c(); int d(); int e(); int f(); int g(); int h();
        int i(); int j(); int k(); int l(); int m(); int n(); int o(); int p();
        int q(); int r(); int s();
        [PreserveSig] int SetPersistedDefaultAudioEndpoint(uint processId, int flow, int role, IntPtr deviceId);
        [PreserveSig] int GetPersistedDefaultAudioEndpoint(uint processId, int flow, int role, [Out, MarshalAs(UnmanagedType.HString)] out string deviceId);
        [PreserveSig] int ClearAllPersistedApplicationDefaultEndpoints();
    }

    [ComImport, Guid("2a59116d-6c4f-45e0-a74f-707e3fef9258"), InterfaceType(ComInterfaceType.InterfaceIsIInspectable)]
    private interface IAudioPolicyConfigFactoryPre21H2
    {
        int a(); int b(); int c(); int d(); int e(); int f(); int g(); int h();
        int i(); int j(); int k(); int l(); int m(); int n(); int o(); int p();
        int q(); int r(); int s();
        [PreserveSig] int SetPersistedDefaultAudioEndpoint(uint processId, int flow, int role, IntPtr deviceId);
        [PreserveSig] int GetPersistedDefaultAudioEndpoint(uint processId, int flow, int role, [Out, MarshalAs(UnmanagedType.HString)] out string deviceId);
        [PreserveSig] int ClearAllPersistedApplicationDefaultEndpoints();
    }

    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    private struct PROCESSENTRY32
    {
        public uint dwSize;
        public uint cntUsage;
        public uint th32ProcessID;
        public IntPtr th32DefaultHeapID;
        public uint th32ModuleID;
        public uint cntThreads;
        public uint th32ParentProcessID;
        public int pcPriClassBase;
        public uint dwFlags;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 260)] public string szExeFile;
    }

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern IntPtr CreateToolhelp32Snapshot(uint flags, uint processId);

    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern bool Process32FirstW(IntPtr snapshot, ref PROCESSENTRY32 entry);

    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    private static extern bool Process32NextW(IntPtr snapshot, ref PROCESSENTRY32 entry);

    [DllImport("kernel32.dll", SetLastError = true)]
    private static extern bool CloseHandle(IntPtr handle);

    [DllImport("combase.dll")]
    private static extern int RoInitialize(int initType);

    [DllImport("combase.dll", CharSet = CharSet.Unicode)]
    private static extern int WindowsCreateString([MarshalAs(UnmanagedType.LPWStr)] string sourceString, uint length, out IntPtr hstring);

    [DllImport("combase.dll")]
    private static extern int WindowsDeleteString(IntPtr hstring);

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
        var iface = flow == 0 ? "#{e6327cad-dcec-4949-ae8a-991e976a79d2}" : "#{2eef81be-33fa-4800-9670-1cd474972c3f}";
        return "\\\\?\\SWD#MMDEVAPI#" + mmDeviceId + iface;
    }

    private static int SetOne(object factory, uint processId, int flow, int role, IntPtr hstring)
    {
        var f21 = factory as IAudioPolicyConfigFactory21H2;
        if (f21 != null) return f21.SetPersistedDefaultAudioEndpoint(processId, flow, role, hstring);
        return ((IAudioPolicyConfigFactoryPre21H2)factory).SetPersistedDefaultAudioEndpoint(processId, flow, role, hstring);
    }

    private static int Persist(object factory, uint processId, int flow, string deviceId)
    {
        IntPtr hstring = IntPtr.Zero;
        int createHr = WindowsCreateString(deviceId, (uint)deviceId.Length, out hstring);
        if (createHr != 0) return createHr;
        try
        {
            foreach (var role in new[] { 1 /*Multimedia*/, 0 /*Console*/ })
            {
                int hr = SetOne(factory, processId, flow, role, hstring);
                if (hr != 0) return hr;
            }
            // Some builds accept Communications and some do not. It is useful
            // for voice apps, but it must never turn two successful required
            // writes into a failure.
            SetOne(factory, processId, flow, 2 /*Communications*/, hstring);
            return 0;
        }
        finally
        {
            if (hstring != IntPtr.Zero) WindowsDeleteString(hstring);
        }
    }

    private static int PersistEither(object factory, uint processId, int flow, string mmDeviceId)
    {
        // Current Windows/EarTrumpet uses the SWD-wrapped MMDevice id. Keep the
        // raw id as a compatibility fallback for builds that still accept it.
        int hr = Persist(factory, processId, flow, SwdId(flow, mmDeviceId));
        if (hr == 0) return 0;
        int raw = Persist(factory, processId, flow, mmDeviceId);
        if (raw == 0) return 0;
        return hr;
    }

    // Electron owns one top-level window but normally several renderer/utility
    // processes. EarTrumpet persists an endpoint against the PID obtained from
    // IAudioSessionControl2, not necessarily the PID that owns the window. Walk
    // the live descendants so the actual audio-session process is always among
    // the PIDs receiving the policy. Processes that disappear mid-scan are
    // harmless: success on any remaining candidate keeps the route valid.
    private static uint[] CandidateProcessIds(uint rootProcessId)
    {
        var ids = new HashSet<uint>();
        var ordered = new List<uint>();
        ids.Add(rootProcessId);
        ordered.Add(rootProcessId);

        var pairs = new List<uint[]>();
        IntPtr snapshot = CreateToolhelp32Snapshot(0x00000002 /*TH32CS_SNAPPROCESS*/, 0);
        if (snapshot == new IntPtr(-1)) return ordered.ToArray();
        try
        {
            var entry = new PROCESSENTRY32();
            entry.dwSize = (uint)Marshal.SizeOf(typeof(PROCESSENTRY32));
            if (Process32FirstW(snapshot, ref entry))
            {
                do
                {
                    pairs.Add(new uint[] { entry.th32ProcessID, entry.th32ParentProcessID });
                    entry.dwSize = (uint)Marshal.SizeOf(typeof(PROCESSENTRY32));
                }
                while (Process32NextW(snapshot, ref entry));
            }
        }
        finally { CloseHandle(snapshot); }

        bool changed;
        do
        {
            changed = false;
            foreach (var pair in pairs)
            {
                uint pid = pair[0], parent = pair[1];
                if (ids.Contains(parent) && ids.Add(pid))
                {
                    ordered.Add(pid);
                    changed = true;
                }
            }
        }
        while (changed);
        return ordered.ToArray();
    }

    private static int PersistProcessTree(object factory, uint rootProcessId, int flow, string mmDeviceId, out int applied, out uint lastPid)
    {
        applied = 0;
        lastPid = rootProcessId;
        int lastHr = unchecked((int)0x80070057);
        foreach (uint candidatePid in CandidateProcessIds(rootProcessId))
        {
            int hr = PersistEither(factory, candidatePid, flow, mmDeviceId);
            lastPid = candidatePid;
            if (hr == 0) applied++;
            else lastHr = hr;
        }
        return applied > 0 ? 0 : lastHr;
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
            int applied;
            uint lastPid;
            int hr = PersistProcessTree(factory, processId, 0, FindEndpointId(0, renderName), out applied, out lastPid);
            if (hr != 0)
                throw new Exception("persisting the playback endpoint failed for the desktop app process tree rooted at PID " + processId + "; last candidate PID " + lastPid + " returned HRESULT 0x" + hr.ToString("X8"));
        }
        if (!string.IsNullOrEmpty(captureName))
        {
            int applied;
            uint lastPid;
            int hr = PersistProcessTree(factory, processId, 1, FindEndpointId(1, captureName), out applied, out lastPid);
            if (hr != 0)
                throw new Exception("persisting the recording endpoint failed for the desktop app process tree rooted at PID " + processId + "; last candidate PID " + lastPid + " returned HRESULT 0x" + hr.ToString("X8"));
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
