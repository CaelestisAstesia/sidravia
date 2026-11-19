param([switch]$AllowBreakaway)

# Assign this dedicated process before starting any descendants.
Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
public static class SidraviaBuildJob {
    static IntPtr job;
    [StructLayout(LayoutKind.Sequential)] struct BasicLimits {
        public long PerProcessTime, PerJobTime;
        public uint Flags;
        public UIntPtr MinimumWorkingSet, MaximumWorkingSet;
        public uint ActiveProcesses;
        public UIntPtr Affinity;
        public uint Priority, Scheduling;
    }
    [StructLayout(LayoutKind.Sequential)] struct IoCounters {
        public ulong ReadOps, WriteOps, OtherOps, ReadBytes, WriteBytes, OtherBytes;
    }
    [StructLayout(LayoutKind.Sequential)] struct Limits {
        public BasicLimits Basic;
        public IoCounters Io;
        public UIntPtr ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory;
    }
    [DllImport("kernel32.dll", SetLastError=true)] static extern IntPtr CreateJobObject(IntPtr attrs, string name);
    [DllImport("kernel32.dll", SetLastError=true)] static extern bool SetInformationJobObject(IntPtr job, int kind, ref Limits info, uint length);
    [DllImport("kernel32.dll", SetLastError=true)] static extern bool AssignProcessToJobObject(IntPtr job, IntPtr process);
    [DllImport("kernel32.dll")] static extern IntPtr GetCurrentProcess();
    [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr handle);
    public static void Ensure() { Ensure(false); }
    public static void Ensure(bool allowBreakaway) {
        if (job != IntPtr.Zero) return;
        IntPtr candidate = CreateJobObject(IntPtr.Zero, null);
        if (candidate == IntPtr.Zero) throw new Win32Exception();
        var info = new Limits(); info.Basic.Flags = 0x2000;
        if (allowBreakaway) info.Basic.Flags |= 0x0800;
        if (!SetInformationJobObject(candidate, 9, ref info, (uint)Marshal.SizeOf(info)) ||
            !AssignProcessToJobObject(candidate, GetCurrentProcess())) {
            int error = Marshal.GetLastWin32Error(); CloseHandle(candidate);
            throw new Win32Exception(error);
        }
        job = candidate;
    }
}
'@
[SidraviaBuildJob]::Ensure([bool]$AllowBreakaway)
