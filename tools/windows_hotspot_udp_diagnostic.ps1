<#
.SYNOPSIS
Captures Windows route and UDP endpoint facts and optionally compares two
credential-free D520 Challenge socket modes.

.EXAMPLE
.\tools\windows_hotspot_udp_diagnostic.ps1 -Mode Snapshot -Label hotspot-off -LocalAddress 59.72.39.104 -ServerAddress 10.100.61.3

.EXAMPLE
.\tools\windows_hotspot_udp_diagnostic.ps1 -Mode Probe -Label hotspot-on -LocalAddress 59.72.39.104 -ServerAddress 10.100.61.3 -AllowCredentialFreeChallenge

.EXAMPLE
.\tools\windows_hotspot_udp_diagnostic.ps1 -Mode Probe -Label hotspot-on -LocalAddress 59.72.39.104 -ServerAddress 10.100.61.3 -InterfaceIndex 8 -AllowCredentialFreeChallenge

.EXAMPLE
.\tools\windows_hotspot_udp_diagnostic.ps1 -Mode Probe -Label hotspot-on -LocalAddress 59.72.39.104 -ServerAddress 10.100.61.3 -InterfaceIndex 8 -UsePacketInfo -AllowCredentialFreeChallenge
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('SelfTest', 'Snapshot', 'Probe')]
    [string]$Mode,

    [string]$Label,
    [string]$LocalAddress,
    [string]$ServerAddress,
    [int]$LocalPort = 61440,
    [int]$ServerPort = 61440,
    [int]$TimeoutMilliseconds = 3000,
    [int]$InterfaceIndex,
    [switch]$UsePacketInfo,
    [switch]$AllowCredentialFreeChallenge
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'
$script:InterfaceIndexRequested = $PSBoundParameters.ContainsKey('InterfaceIndex')
$script:PacketInfoRequested = [bool]$UsePacketInfo
$script:IpUnicastIfOptionName = 31

$script:PacketInfoHelperSource = @'
using System;
using System.Net;
using System.Net.Sockets;
using System.Runtime.InteropServices;

public static class SidraviaPacketInfoSender
{
    private const int SocketError = -1;
    private const int SioGetExtensionFunctionPointer = unchecked((int)0xC8000006);
    private const int IpProtoIp = 0;
    private const int IpPacketInfo = 19;
    private static readonly Guid WsaIdWsaSendMsg =
        new Guid("a441e712-754f-43ca-84a7-0dee44cf606d");

    [StructLayout(LayoutKind.Sequential)]
    private struct WsaBuffer
    {
        public UIntPtr Length;
        public IntPtr Buffer;
    }

    [StructLayout(LayoutKind.Sequential)]
    private struct WsaMessage
    {
        public IntPtr Name;
        public int NameLength;
        public IntPtr Buffers;
        public uint BufferCount;
        public WsaBuffer Control;
        public uint Flags;
    }

    [UnmanagedFunctionPointer(CallingConvention.StdCall)]
    private delegate int WsaSendMsgDelegate(
        IntPtr socket,
        ref WsaMessage message,
        uint flags,
        out uint bytesSent,
        IntPtr overlapped,
        IntPtr completionRoutine);

    [DllImport("Ws2_32.dll", SetLastError = true)]
    private static extern int WSAIoctl(
        IntPtr socket,
        int controlCode,
        ref Guid inputBuffer,
        int inputBufferLength,
        out IntPtr outputBuffer,
        int outputBufferLength,
        out int bytesReturned,
        IntPtr overlapped,
        IntPtr completionRoutine);

    [DllImport("Ws2_32.dll")]
    private static extern int WSAGetLastError();

    public sealed class SendResult
    {
        public bool Success;
        public int ErrorCode;
    }

    public sealed class LayoutInspection
    {
        public int PointerSize;
        public int HeaderLength;
        public int DataOffset;
        public int ControlLength;
        public int Level;
        public int Type;
        public byte[] SourceAddressBytes;
        public int InterfaceIndex;
    }

    private static int Align(int length)
    {
        int alignment = IntPtr.Size;
        return (length + alignment - 1) & ~(alignment - 1);
    }

    private static IntPtr BuildControl(
        IPAddress sourceAddress,
        int interfaceIndex,
        out int headerLength,
        out int dataOffset,
        out int controlLength)
    {
        byte[] addressBytes = sourceAddress.GetAddressBytes();
        if (addressBytes.Length != 4)
        {
            throw new ArgumentException("IPv4 source required", "sourceAddress");
        }

        headerLength = IntPtr.Size + 8;
        dataOffset = Align(headerLength);
        int packetInfoLength = 8;
        controlLength = dataOffset + Align(packetInfoLength);
        int messageLength = dataOffset + packetInfoLength;
        IntPtr control = Marshal.AllocHGlobal(controlLength);
        for (int i = 0; i < controlLength; i++)
        {
            Marshal.WriteByte(control, i, 0);
        }

        Marshal.WriteIntPtr(control, 0, new IntPtr(messageLength));
        Marshal.WriteInt32(control, IntPtr.Size, IpProtoIp);
        Marshal.WriteInt32(control, IntPtr.Size + 4, IpPacketInfo);
        for (int i = 0; i < addressBytes.Length; i++)
        {
            Marshal.WriteByte(control, dataOffset + i, addressBytes[i]);
        }
        Marshal.WriteInt32(control, dataOffset + 4, interfaceIndex);
        return control;
    }

    private static IntPtr BuildSockaddr(IPEndPoint destination)
    {
        byte[] addressBytes = destination.Address.GetAddressBytes();
        if (addressBytes.Length != 4)
        {
            throw new ArgumentException("IPv4 destination required", "destination");
        }
        IntPtr address = Marshal.AllocHGlobal(16);
        for (int i = 0; i < 16; i++)
        {
            Marshal.WriteByte(address, i, 0);
        }
        Marshal.WriteInt16(address, 0, 2);
        Marshal.WriteByte(address, 2, (byte)((destination.Port >> 8) & 0xff));
        Marshal.WriteByte(address, 3, (byte)(destination.Port & 0xff));
        for (int i = 0; i < addressBytes.Length; i++)
        {
            Marshal.WriteByte(address, 4 + i, addressBytes[i]);
        }
        return address;
    }

    public static LayoutInspection Inspect(IPAddress sourceAddress, int interfaceIndex)
    {
        IntPtr control = IntPtr.Zero;
        try
        {
            int headerLength;
            int dataOffset;
            int controlLength;
            control = BuildControl(
                sourceAddress,
                interfaceIndex,
                out headerLength,
                out dataOffset,
                out controlLength);
            byte[] sourceBytes = new byte[4];
            for (int i = 0; i < sourceBytes.Length; i++)
            {
                sourceBytes[i] = Marshal.ReadByte(control, dataOffset + i);
            }
            return new LayoutInspection
            {
                PointerSize = IntPtr.Size,
                HeaderLength = headerLength,
                DataOffset = dataOffset,
                ControlLength = controlLength,
                Level = Marshal.ReadInt32(control, IntPtr.Size),
                Type = Marshal.ReadInt32(control, IntPtr.Size + 4),
                SourceAddressBytes = sourceBytes,
                InterfaceIndex = Marshal.ReadInt32(control, dataOffset + 4)
            };
        }
        finally
        {
            if (control != IntPtr.Zero)
            {
                Marshal.FreeHGlobal(control);
            }
        }
    }

    public static SendResult Send(
        Socket socket,
        byte[] payload,
        IPAddress sourceAddress,
        int interfaceIndex,
        IPEndPoint destination)
    {
        IntPtr control = IntPtr.Zero;
        IntPtr payloadBuffer = IntPtr.Zero;
        IntPtr wsaBufferPointer = IntPtr.Zero;
        IntPtr name = IntPtr.Zero;
        try
        {
            int headerLength;
            int dataOffset;
            int controlLength;
            control = BuildControl(
                sourceAddress,
                interfaceIndex,
                out headerLength,
                out dataOffset,
                out controlLength);

            payloadBuffer = Marshal.AllocHGlobal(payload.Length);
            Marshal.Copy(payload, 0, payloadBuffer, payload.Length);
            WsaBuffer payloadWsaBuffer = new WsaBuffer
            {
                Length = new UIntPtr((uint)payload.Length),
                Buffer = payloadBuffer
            };
            wsaBufferPointer = Marshal.AllocHGlobal(Marshal.SizeOf(typeof(WsaBuffer)));
            Marshal.StructureToPtr(payloadWsaBuffer, wsaBufferPointer, false);

            int nameLength = 0;
            if (destination != null)
            {
                name = BuildSockaddr(destination);
                nameLength = 16;
            }

            IntPtr functionPointer;
            int bytesReturned;
            Guid functionId = WsaIdWsaSendMsg;
            int ioctlResult = WSAIoctl(
                socket.Handle,
                SioGetExtensionFunctionPointer,
                ref functionId,
                Marshal.SizeOf(typeof(Guid)),
                out functionPointer,
                IntPtr.Size,
                out bytesReturned,
                IntPtr.Zero,
                IntPtr.Zero);
            if (ioctlResult == SocketError)
            {
                return new SendResult { Success = false, ErrorCode = WSAGetLastError() };
            }

            WsaSendMsgDelegate sendMessage =
                (WsaSendMsgDelegate)Marshal.GetDelegateForFunctionPointer(
                    functionPointer,
                    typeof(WsaSendMsgDelegate));
            WsaMessage message = new WsaMessage
            {
                Name = name,
                NameLength = nameLength,
                Buffers = wsaBufferPointer,
                BufferCount = 1,
                Control = new WsaBuffer
                {
                    Length = new UIntPtr((uint)controlLength),
                    Buffer = control
                },
                Flags = 0
            };
            uint bytesSent;
            int sendResult = sendMessage(
                socket.Handle,
                ref message,
                0,
                out bytesSent,
                IntPtr.Zero,
                IntPtr.Zero);
            if (sendResult == SocketError)
            {
                return new SendResult { Success = false, ErrorCode = WSAGetLastError() };
            }
            if (bytesSent != payload.Length)
            {
                return new SendResult { Success = false, ErrorCode = 10040 };
            }
            return new SendResult { Success = true, ErrorCode = 0 };
        }
        finally
        {
            if (name != IntPtr.Zero)
            {
                Marshal.FreeHGlobal(name);
            }
            if (wsaBufferPointer != IntPtr.Zero)
            {
                Marshal.FreeHGlobal(wsaBufferPointer);
            }
            if (payloadBuffer != IntPtr.Zero)
            {
                Marshal.FreeHGlobal(payloadBuffer);
            }
            if (control != IntPtr.Zero)
            {
                Marshal.FreeHGlobal(control);
            }
        }
    }
}
'@

function Initialize-PacketInfoHelper {
    if ($null -eq ('SidraviaPacketInfoSender' -as [type])) {
        $null = Add-Type -TypeDefinition $script:PacketInfoHelperSource -Language CSharp -ErrorAction Stop
    }
}

$script:SchemaFieldNames = @(
    'schemaVersion',
    'capturedAtUtc',
    'label',
    'inputs',
    'route',
    'interface',
    'localAddressMappings',
    'udpEndpoints',
    'probePreflight',
    'probeOrder',
    'probes'
)

function Test-IPv4Literal {
    param([string]$Value)

    if ([string]::IsNullOrWhiteSpace($Value) -or
        $Value -notmatch '^\d{1,3}(\.\d{1,3}){3}$') {
        return $false
    }
    foreach ($part in $Value.Split('.')) {
        $number = 0
        if (-not [int]::TryParse($part, [ref]$number) -or
            $number -lt 0 -or $number -gt 255) {
            return $false
        }
    }
    $parsed = $null
    return [System.Net.IPAddress]::TryParse($Value, [ref]$parsed) -and
        $parsed.AddressFamily -eq [System.Net.Sockets.AddressFamily]::InterNetwork
}

function ConvertTo-IPv4Address {
    param([string]$Value)

    if (-not (Test-IPv4Literal -Value $Value)) {
        throw 'invalid_ipv4_literal'
    }
    return [System.Net.IPAddress]::Parse($Value)
}

function Test-PortValue {
    param([int]$Value)

    return $Value -ge 1 -and $Value -le 65535
}

function Test-TimeoutValue {
    param([int]$Value)

    return $Value -ge 100 -and $Value -le 30000
}

function Test-InterfaceIndexValue {
    param([int]$Value)

    return $Value -ge 1 -and $Value -le 0x00ffffff
}

function Test-InterfaceIndexModeCompatibility {
    param(
        [string]$RequestedMode,
        [bool]$WasRequested
    )

    return -not $WasRequested -or $RequestedMode -eq 'Probe'
}

function Get-PacketInfoArgumentStatus {
    param(
        [string]$RequestedMode,
        [bool]$PacketInfoWasRequested,
        [bool]$InterfaceWasRequested,
        [string]$RequestedLocalAddress
    )

    if (-not $PacketInfoWasRequested) {
        return 'valid'
    }
    if ($RequestedMode -ne 'Probe') {
        return 'packet_info_only_for_probe'
    }
    if (-not $InterfaceWasRequested) {
        return 'packet_info_requires_interface_index'
    }
    if ($RequestedLocalAddress -eq '0.0.0.0') {
        return 'packet_info_requires_concrete_local_address'
    }
    return 'valid'
}

function ConvertTo-UnicastInterfaceOptionBytes {
    param([int]$Value)

    if (-not (Test-InterfaceIndexValue $Value)) {
        throw 'invalid_interface_index'
    }
    return [byte[]]@(
        [byte](($Value -shr 24) -band 0xff),
        [byte](($Value -shr 16) -band 0xff),
        [byte](($Value -shr 8) -band 0xff),
        [byte]($Value -band 0xff)
    )
}

function New-InputProjection {
    param(
        [string]$ProjectedLocalAddress,
        [int]$ProjectedLocalPort,
        [string]$ProjectedServerAddress,
        [int]$ProjectedServerPort,
        [int]$ProjectedTimeoutMilliseconds,
        [bool]$InterfaceWasRequested,
        [int]$ProjectedInterfaceIndex,
        [bool]$PacketInfoWasRequested
    )

    return [pscustomobject][ordered]@{
        localAddress = $ProjectedLocalAddress
        localPort = $ProjectedLocalPort
        serverAddress = $ProjectedServerAddress
        serverPort = $ProjectedServerPort
        timeoutMilliseconds = $ProjectedTimeoutMilliseconds
        interfaceIndex = if ($InterfaceWasRequested) { $ProjectedInterfaceIndex } else { $null }
        usePacketInfo = $PacketInfoWasRequested
    }
}

function New-ChallengeRequest {
    param(
        [long]$UnixSeconds,
        [int]$Offset
    )

    if ($Offset -lt 0x0f -or $Offset -gt 0xff) {
        throw 'invalid_challenge_offset'
    }
    $seed = [uint16](($UnixSeconds + $Offset) % 0xffff)
    $request = New-Object byte[] 20
    $request[0] = 0x01
    $request[1] = 0x02
    $request[2] = [byte]($seed -band 0xff)
    $request[3] = [byte](($seed -shr 8) -band 0xff)
    $request[4] = 0x09
    return $request
}

function New-RandomChallengeRequest {
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $sample = New-Object byte[] 1
        do {
            $rng.GetBytes($sample)
        } while ($sample[0] -gt 240)
        $offset = 0x0f + [int]$sample[0]
    }
    finally {
        $rng.Dispose()
    }
    $now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
    return New-ChallengeRequest -UnixSeconds $now -Offset $offset
}

function Assert-SelfTest {
    param(
        [bool]$Condition,
        [string]$Name
    )

    if (-not $Condition) {
        throw "self_test_failed_$Name"
    }
}

function Invoke-SelfTest {
    Initialize-PacketInfoHelper

    $low = New-ChallengeRequest -UnixSeconds 0x1234 -Offset 0x0f
    Assert-SelfTest ($low.Length -eq 20) 'challenge_length'
    Assert-SelfTest ($low[0] -eq 0x01 -and $low[1] -eq 0x02 -and
        $low[4] -eq 0x09) 'challenge_fixed_bytes'
    Assert-SelfTest ($low[2] -eq 0x43 -and $low[3] -eq 0x12) 'seed_little_endian'
    Assert-SelfTest ((@($low[5..19] | Where-Object { $_ -ne 0 }).Count) -eq 0) 'zero_padding'

    $high = New-ChallengeRequest -UnixSeconds 0 -Offset 0xff
    Assert-SelfTest ($high[2] -eq 0xff -and $high[3] -eq 0x00) 'inclusive_high_offset'

    $outsideRejected = $false
    try {
        $null = New-ChallengeRequest -UnixSeconds 0 -Offset 0x0e
    }
    catch {
        $outsideRejected = $true
    }
    Assert-SelfTest $outsideRejected 'outside_offset_rejected'

    Assert-SelfTest (Test-IPv4Literal '59.72.39.104') 'valid_ipv4'
    Assert-SelfTest (-not (Test-IPv4Literal '2001:db8::1')) 'ipv6_rejected'
    Assert-SelfTest (-not (Test-IPv4Literal '10.100.61.999')) 'invalid_ipv4_rejected'
    Assert-SelfTest ((Test-PortValue 1) -and (Test-PortValue 65535) -and
        -not (Test-PortValue 0) -and -not (Test-PortValue 65536)) 'port_bounds'
    Assert-SelfTest ((Test-TimeoutValue 100) -and (Test-TimeoutValue 30000) -and
        -not (Test-TimeoutValue 99) -and -not (Test-TimeoutValue 30001)) 'timeout_bounds'

    $expectedFields = 'schemaVersion,capturedAtUtc,label,inputs,route,interface,localAddressMappings,udpEndpoints,probePreflight,probeOrder,probes'
    Assert-SelfTest (($script:SchemaFieldNames -join ',') -eq $expectedFields) 'schema_fields'

    $queryFailed = [pscustomobject][ordered]@{
        status = 'query_failed'
        rows = @()
    }
    $queryFailedDecision = Get-ProbePreflightDecision $queryFailed
    Assert-SelfTest ($queryFailedDecision.status -eq 'endpoint_query_failed' -and
        @($queryFailedDecision.owners).Count -eq 0) 'endpoint_query_failure_blocks'

    $occupiedRow = [pscustomobject][ordered]@{
        localAddress = '0.0.0.0'
        localPort = 61440
        owningPid = 1234
        processName = 'example'
    }
    $found = [pscustomobject][ordered]@{
        status = 'found'
        rows = @($occupiedRow)
    }
    $foundDecision = Get-ProbePreflightDecision $found
    Assert-SelfTest ($foundDecision.status -eq 'local_port_in_use' -and
        @($foundDecision.owners).Count -eq 1) 'endpoint_owner_blocks'

    $notFound = [pscustomobject][ordered]@{
        status = 'not_found'
        rows = @()
    }
    $notFoundDecision = Get-ProbePreflightDecision $notFound
    Assert-SelfTest ($notFoundDecision.status -eq 'ready' -and
        @($notFoundDecision.owners).Count -eq 0) 'verified_empty_allows_probe'

    Assert-SelfTest ((Test-InterfaceIndexValue 1) -and
        (Test-InterfaceIndexValue 8) -and
        (Test-InterfaceIndexValue 0x00ffffff)) 'valid_interface_indices'
    Assert-SelfTest (-not (Test-InterfaceIndexValue 0) -and
        -not (Test-InterfaceIndexValue -1) -and
        -not (Test-InterfaceIndexValue 0x01000000)) 'invalid_interface_indices'

    [byte[]]$interface8 = ConvertTo-UnicastInterfaceOptionBytes 8
    Assert-SelfTest (($interface8 -join ',') -eq '0,0,0,8') 'interface_8_network_order'
    [byte[]]$interface010203 = ConvertTo-UnicastInterfaceOptionBytes 0x00010203
    Assert-SelfTest (($interface010203 -join ',') -eq '0,1,2,3') 'interface_010203_network_order'
    Assert-SelfTest ($script:IpUnicastIfOptionName -eq 31) 'ip_unicast_if_option_name'

    Assert-SelfTest (-not (Test-InterfaceIndexModeCompatibility 'SelfTest' $true)) 'self_test_rejects_interface_index'
    Assert-SelfTest (-not (Test-InterfaceIndexModeCompatibility 'Snapshot' $true)) 'snapshot_rejects_interface_index'
    Assert-SelfTest (Test-InterfaceIndexModeCompatibility 'Probe' $true) 'probe_accepts_interface_index'

    $inputsWithoutInterface = New-InputProjection '59.72.39.104' 61440 '10.100.61.3' 61440 3000 $false 0 $false
    Assert-SelfTest ($null -eq $inputsWithoutInterface.interfaceIndex) 'input_interface_index_null'
    Assert-SelfTest ($inputsWithoutInterface.usePacketInfo -eq $false) 'input_packet_info_false'
    $inputsWithInterface = New-InputProjection '59.72.39.104' 61440 '10.100.61.3' 61440 3000 $true 8 $true
    Assert-SelfTest ($inputsWithInterface.interfaceIndex -eq 8) 'input_interface_index_value'
    Assert-SelfTest ($inputsWithInterface.usePacketInfo -eq $true) 'input_packet_info_true'

    Assert-SelfTest ((Get-PacketInfoArgumentStatus 'SelfTest' $true $true '59.72.39.104') -eq
        'packet_info_only_for_probe') 'self_test_rejects_packet_info'
    Assert-SelfTest ((Get-PacketInfoArgumentStatus 'Snapshot' $true $true '59.72.39.104') -eq
        'packet_info_only_for_probe') 'snapshot_rejects_packet_info'
    Assert-SelfTest ((Get-PacketInfoArgumentStatus 'Probe' $true $false '59.72.39.104') -eq
        'packet_info_requires_interface_index') 'packet_info_requires_interface'
    Assert-SelfTest ((Get-PacketInfoArgumentStatus 'Probe' $true $true '0.0.0.0') -eq
        'packet_info_requires_concrete_local_address') 'packet_info_rejects_wildcard'
    Assert-SelfTest ((Get-PacketInfoArgumentStatus 'Probe' $true $true '59.72.39.104') -eq
        'valid') 'packet_info_valid_arguments'

    $layout = [SidraviaPacketInfoSender]::Inspect(
        [System.Net.IPAddress]::Parse('59.72.39.104'),
        8
    )
    Assert-SelfTest ($layout.DataOffset -ge $layout.HeaderLength -and
        ($layout.DataOffset % $layout.PointerSize) -eq 0 -and
        ($layout.ControlLength % $layout.PointerSize) -eq 0) 'packet_info_native_alignment'
    Assert-SelfTest ($layout.Level -eq 0) 'packet_info_level'
    Assert-SelfTest ($layout.Type -eq 19) 'packet_info_type'
    Assert-SelfTest (($layout.SourceAddressBytes -join ',') -eq '59,72,39,104') 'packet_info_source_bytes'
    Assert-SelfTest ($layout.InterfaceIndex -eq 8) 'packet_info_interface_index'
}

function Get-ObjectPropertyValue {
    param(
        [object]$InputObject,
        [string[]]$Names
    )

    if ($null -eq $InputObject) {
        return $null
    }
    foreach ($name in $Names) {
        $property = $InputObject.PSObject.Properties[$name]
        if ($null -ne $property -and $null -ne $property.Value) {
            return $property.Value
        }
    }
    return $null
}

function Get-ProcessNameSafely {
    param([uint32]$ProcessId)

    if ($ProcessId -eq 0) {
        return $null
    }
    try {
        return (Get-Process -Id $ProcessId -ErrorAction Stop).ProcessName
    }
    catch {
        return $null
    }
}

function Get-UdpEndpointFacts {
    param([int]$Port)

    $rows = @()
    try {
        $allEndpoints = @(Get-NetUDPEndpoint -ErrorAction Stop)
        $found = @($allEndpoints | Where-Object { [int]$_.LocalPort -eq $Port })
    }
    catch {
        return [pscustomobject][ordered]@{
            status = 'query_failed'
            rows = @()
        }
    }
    foreach ($endpoint in $found) {
        $pidValue = Get-ObjectPropertyValue $endpoint @('OwningProcess')
        $pidNumber = if ($null -eq $pidValue) { $null } else { [uint32]$pidValue }
        $rows += [pscustomobject][ordered]@{
            localAddress = [string](Get-ObjectPropertyValue $endpoint @('LocalAddress'))
            localPort = [int](Get-ObjectPropertyValue $endpoint @('LocalPort'))
            owningPid = $pidNumber
            processName = if ($null -eq $pidNumber) { $null } else { Get-ProcessNameSafely $pidNumber }
        }
    }
    $sortedRows = @($rows | Sort-Object localAddress, localPort, owningPid)
    return [pscustomobject][ordered]@{
        status = if ($sortedRows.Count -gt 0) { 'found' } else { 'not_found' }
        rows = $sortedRows
    }
}

function Get-ProbePreflightDecision {
    param([object]$EndpointQuery)

    $rows = @($EndpointQuery.rows)
    if ($EndpointQuery.status -eq 'query_failed') {
        return [pscustomobject][ordered]@{
            status = 'endpoint_query_failed'
            owners = @()
        }
    }
    if ($rows.Count -gt 0) {
        return [pscustomobject][ordered]@{
            status = 'local_port_in_use'
            owners = $rows
        }
    }
    if ($EndpointQuery.status -eq 'not_found') {
        return [pscustomobject][ordered]@{
            status = 'ready'
            owners = @()
        }
    }
    return [pscustomobject][ordered]@{
        status = 'endpoint_query_failed'
        owners = @()
    }
}

function Get-SnapshotFacts {
    param(
        [string]$RequestedLocalAddress,
        [string]$RequestedServerAddress,
        [int]$RequestedLocalPort
    )

    $routeObject = $null
    $routeStatus = 'not_found'
    try {
        $routeResults = @(Find-NetRoute -RemoteIPAddress $RequestedServerAddress -ErrorAction Stop)
        if ($routeResults.Count -gt 0) {
            $routeObject = $routeResults[0]
            $routeStatus = 'found'
        }
    }
    catch {
        $routeStatus = 'query_failed'
    }

    $interfaceIndex = Get-ObjectPropertyValue $routeObject @('InterfaceIndex', 'ifIndex')
    $route = [pscustomobject][ordered]@{
        status = $routeStatus
        selectedSourceAddress = Get-ObjectPropertyValue $routeObject @('IPAddress', 'SelectedSourceAddress', 'SourceAddress')
        destinationPrefix = Get-ObjectPropertyValue $routeObject @('DestinationPrefix')
        nextHop = Get-ObjectPropertyValue $routeObject @('NextHop')
        interfaceIndex = $interfaceIndex
        interfaceAlias = Get-ObjectPropertyValue $routeObject @('InterfaceAlias')
        routeMetric = Get-ObjectPropertyValue $routeObject @('RouteMetric')
    }

    $interfaceObject = $null
    $interfaceStatus = if ($null -eq $interfaceIndex) { 'not_found' } else { 'query_failed' }
    if ($null -ne $interfaceIndex) {
        try {
            $interfaceResults = @(Get-NetIPInterface -AddressFamily IPv4 -InterfaceIndex $interfaceIndex -ErrorAction Stop)
            if ($interfaceResults.Count -gt 0) {
                $interfaceObject = $interfaceResults[0]
                $interfaceStatus = 'found'
            }
            else {
                $interfaceStatus = 'not_found'
            }
        }
        catch {
            $interfaceStatus = 'query_failed'
        }
    }
    $interface = [pscustomobject][ordered]@{
        status = $interfaceStatus
        interfaceAlias = Get-ObjectPropertyValue $interfaceObject @('InterfaceAlias')
        interfaceIndex = Get-ObjectPropertyValue $interfaceObject @('InterfaceIndex', 'ifIndex')
        interfaceMetric = Get-ObjectPropertyValue $interfaceObject @('InterfaceMetric')
        connectionState = Get-ObjectPropertyValue $interfaceObject @('ConnectionState')
    }

    $mappingRows = @()
    try {
        $mappings = @(Get-NetIPAddress -AddressFamily IPv4 -IPAddress $RequestedLocalAddress -ErrorAction Stop)
    }
    catch {
        $mappings = @()
    }
    foreach ($mapping in $mappings) {
        $mappingRows += [pscustomobject][ordered]@{
            ipAddress = Get-ObjectPropertyValue $mapping @('IPAddress')
            interfaceIndex = Get-ObjectPropertyValue $mapping @('InterfaceIndex', 'ifIndex')
            interfaceAlias = Get-ObjectPropertyValue $mapping @('InterfaceAlias')
        }
    }

    return [pscustomobject][ordered]@{
        route = $route
        interface = $interface
        localAddressMappings = @($mappingRows | Sort-Object interfaceIndex, ipAddress)
        udpEndpoints = Get-UdpEndpointFacts -Port $RequestedLocalPort
    }
}

function New-ResetControlResult {
    param(
        [string]$Name,
        [bool]$Succeeded,
        [object]$ErrorCode
    )

    return [pscustomobject][ordered]@{
        name = $Name
        succeeded = $Succeeded
        socketErrorCode = $ErrorCode
    }
}

function Get-SocketException {
    param([System.Exception]$Exception)

    $current = $Exception
    while ($null -ne $current) {
        if ($current -is [System.Net.Sockets.SocketException]) {
            return $current
        }
        $current = $current.InnerException
    }
    return $null
}

function Disable-UdpResetControl {
    param(
        [System.Net.Sockets.Socket]$Socket,
        [int]$ControlCode,
        [string]$Name
    )

    try {
        $null = $Socket.IOControl($ControlCode, [byte[]](0, 0, 0, 0), $null)
        return New-ResetControlResult -Name $Name -Succeeded $true -ErrorCode $null
    }
    catch {
        $socketException = Get-SocketException $_.Exception
        $errorCode = if ($null -eq $socketException) { 'unknown' } else { $socketException.SocketErrorCode.ToString() }
        return New-ResetControlResult -Name $Name -Succeeded $false -ErrorCode $errorCode
    }
}

function Set-UnicastInterfaceOption {
    param(
        [System.Net.Sockets.Socket]$Socket,
        [object]$RequestedInterfaceIndex
    )

    if ($null -eq $RequestedInterfaceIndex) {
        return [pscustomobject][ordered]@{
            status = 'not_requested'
            requestedInterfaceIndex = $null
            socketErrorCode = $null
        }
    }
    try {
        [byte[]]$optionBytes = ConvertTo-UnicastInterfaceOptionBytes ([int]$RequestedInterfaceIndex)
        $optionName = [System.Net.Sockets.SocketOptionName]$script:IpUnicastIfOptionName
        $Socket.SetSocketOption(
            [System.Net.Sockets.SocketOptionLevel]::IP,
            $optionName,
            $optionBytes
        )
        return [pscustomobject][ordered]@{
            status = 'applied'
            requestedInterfaceIndex = [int]$RequestedInterfaceIndex
            socketErrorCode = $null
        }
    }
    catch {
        $socketException = Get-SocketException $_.Exception
        $errorCode = if ($null -eq $socketException) { 'unknown' } else { $socketException.SocketErrorCode.ToString() }
        return [pscustomobject][ordered]@{
            status = 'failed'
            requestedInterfaceIndex = [int]$RequestedInterfaceIndex
            socketErrorCode = $errorCode
        }
    }
}

function New-EndpointFact {
    param([System.Net.EndPoint]$Endpoint)

    if ($null -eq $Endpoint) {
        return $null
    }
    $ipEndpoint = [System.Net.IPEndPoint]$Endpoint
    return [pscustomobject][ordered]@{
        address = $ipEndpoint.Address.ToString()
        port = $ipEndpoint.Port
    }
}

function New-ReceivedDatagramFact {
    param(
        [byte[]]$Buffer,
        [int]$Length,
        [System.Net.EndPoint]$Sender
    )

    $firstOpcode = if ($Length -gt 0) { [int]$Buffer[0] } else { $null }
    return [pscustomobject][ordered]@{
        length = $Length
        firstOpcode = $firstOpcode
        meetsChallengeMinimum = ($Length -ge 8 -and $firstOpcode -eq 0x02)
        sender = New-EndpointFact $Sender
    }
}

function Invoke-ProbeAttempt {
    param(
        [ValidateSet('unconnected', 'connected')]
        [string]$SocketMode,
        [System.Net.IPAddress]$LocalIPAddress,
        [int]$RequestedLocalPort,
        [System.Net.IPAddress]$ServerIPAddress,
        [int]$RequestedServerPort,
        [int]$RequestedTimeoutMilliseconds,
        [object]$RequestedInterfaceIndex,
        [bool]$UsePacketInfoForAttempt
    )

    $started = [DateTimeOffset]::UtcNow
    $stopwatch = [System.Diagnostics.Stopwatch]::StartNew()
    $socket = $null
    $outcome = 'socket_error'
    $socketErrorCode = $null
    $socketErrorCategory = $null
    $boundEndpoint = $null
    $connectedEndpoint = $null
    $received = $null
    $resetControls = @()
    $unicastInterface = [pscustomobject][ordered]@{
        status = 'not_requested'
        requestedInterfaceIndex = $null
        socketErrorCode = $null
    }
    $packetInfo = if ($UsePacketInfoForAttempt) {
        [pscustomobject][ordered]@{
            status = 'failed'
            requestedSourceAddress = $LocalIPAddress.ToString()
            requestedInterfaceIndex = [int]$RequestedInterfaceIndex
            winsockErrorCode = 'unknown'
        }
    }
    else {
        [pscustomobject][ordered]@{
            status = 'not_requested'
            requestedSourceAddress = $null
            requestedInterfaceIndex = $null
            winsockErrorCode = $null
        }
    }
    $target = New-Object System.Net.IPEndPoint($ServerIPAddress, $RequestedServerPort)

    try {
        $socket = New-Object System.Net.Sockets.Socket(
            [System.Net.Sockets.AddressFamily]::InterNetwork,
            [System.Net.Sockets.SocketType]::Dgram,
            [System.Net.Sockets.ProtocolType]::Udp
        )
        $socket.ExclusiveAddressUse = $true
        $socket.SetSocketOption(
            [System.Net.Sockets.SocketOptionLevel]::Socket,
            [System.Net.Sockets.SocketOptionName]::ReuseAddress,
            $false
        )
        $socket.EnableBroadcast = $true
        $socket.SendTimeout = $RequestedTimeoutMilliseconds
        $socket.ReceiveTimeout = $RequestedTimeoutMilliseconds

        $resetControls = @(
            Disable-UdpResetControl $socket ([int]-1744830452) 'SIO_UDP_CONNRESET'
            Disable-UdpResetControl $socket ([int]-1744830449) 'SIO_UDP_NETRESET'
        )
        $failedReset = @($resetControls | Where-Object { -not $_.succeeded })
        if ($failedReset.Count -gt 0) {
            $socketErrorCode = $failedReset[0].socketErrorCode
            $socketErrorCategory = 'reset_control'
        }
        else {
            if (-not $UsePacketInfoForAttempt) {
                $unicastInterface = Set-UnicastInterfaceOption $socket $RequestedInterfaceIndex
            }
            if ($unicastInterface.status -eq 'failed') {
                $socketErrorCode = $unicastInterface.socketErrorCode
                $socketErrorCategory = 'unicast_interface'
            }
            else {
                $local = New-Object System.Net.IPEndPoint($LocalIPAddress, $RequestedLocalPort)
                $socket.Bind($local)
                $boundEndpoint = New-EndpointFact $socket.LocalEndPoint
                $request = New-RandomChallengeRequest
                $buffer = New-Object byte[] 65535
                $sendSucceeded = $false

                if ($SocketMode -eq 'connected') {
                    $socket.Connect($target)
                    $connectedEndpoint = New-EndpointFact $socket.RemoteEndPoint
                }

                if ($UsePacketInfoForAttempt) {
                    try {
                        $packetDestination = if ($SocketMode -eq 'unconnected') { $target } else { $null }
                        $sendResult = [SidraviaPacketInfoSender]::Send(
                            $socket,
                            [byte[]]$request,
                            $LocalIPAddress,
                            [int]$RequestedInterfaceIndex,
                            $packetDestination
                        )
                        if ($sendResult.Success) {
                            $packetInfo = [pscustomobject][ordered]@{
                                status = 'applied'
                                requestedSourceAddress = $LocalIPAddress.ToString()
                                requestedInterfaceIndex = [int]$RequestedInterfaceIndex
                                winsockErrorCode = $null
                            }
                            $sendSucceeded = $true
                        }
                        else {
                            $packetInfo = [pscustomobject][ordered]@{
                                status = 'failed'
                                requestedSourceAddress = $LocalIPAddress.ToString()
                                requestedInterfaceIndex = [int]$RequestedInterfaceIndex
                                winsockErrorCode = [int]$sendResult.ErrorCode
                            }
                            $socketErrorCode = [int]$sendResult.ErrorCode
                            $socketErrorCategory = 'packet_info'
                        }
                    }
                    catch {
                        $packetInfo = [pscustomobject][ordered]@{
                            status = 'failed'
                            requestedSourceAddress = $LocalIPAddress.ToString()
                            requestedInterfaceIndex = [int]$RequestedInterfaceIndex
                            winsockErrorCode = 'unknown'
                        }
                        $socketErrorCode = 'unknown'
                        $socketErrorCategory = 'packet_info'
                    }
                }
                elseif ($SocketMode -eq 'unconnected') {
                    $null = $socket.SendTo($request, $target)
                    $sendSucceeded = $true
                }
                else {
                    $null = $socket.Send($request)
                    $sendSucceeded = $true
                }

                if ($sendSucceeded -and $SocketMode -eq 'unconnected') {
                    [System.Net.EndPoint]$sender = New-Object System.Net.IPEndPoint(
                        [System.Net.IPAddress]::Any,
                        0
                    )
                    $length = $socket.ReceiveFrom($buffer, [ref]$sender)
                    $received = New-ReceivedDatagramFact $buffer $length $sender
                }
                elseif ($sendSucceeded) {
                    $length = $socket.Receive($buffer)
                    $received = New-ReceivedDatagramFact $buffer $length $null
                }
                if ($sendSucceeded) {
                    $outcome = if ($received.meetsChallengeMinimum) {
                        'challenge_response'
                    }
                    else {
                        'non_challenge_datagram'
                    }
                }
            }
        }
    }
    catch {
        $socketException = Get-SocketException $_.Exception
        if ($null -ne $socketException) {
            $socketErrorCode = $socketException.SocketErrorCode.ToString()
            $socketErrorCategory = 'socket'
            if ($socketException.SocketErrorCode -eq [System.Net.Sockets.SocketError]::TimedOut) {
                $outcome = 'timeout'
                $socketErrorCode = $null
                $socketErrorCategory = $null
            }
            else {
                $outcome = 'socket_error'
            }
        }
        else {
            $socketErrorCode = 'unknown'
            $socketErrorCategory = 'runtime'
            $outcome = 'socket_error'
        }
    }
    finally {
        if ($null -ne $socket) {
            $socket.Dispose()
        }
        $stopwatch.Stop()
    }

    return [pscustomobject][ordered]@{
        startedAtUtc = $started.UtcDateTime.ToString('o')
        endedAtUtc = [DateTimeOffset]::UtcNow.UtcDateTime.ToString('o')
        elapsedMilliseconds = [long]$stopwatch.ElapsedMilliseconds
        mode = $SocketMode
        boundLocalEndpoint = $boundEndpoint
        targetEndpoint = New-EndpointFact $target
        connectedEndpoint = $connectedEndpoint
        resetControls = $resetControls
        unicastInterface = $unicastInterface
        packetInfo = $packetInfo
        outcome = $outcome
        socketErrorCode = $socketErrorCode
        socketErrorCategory = $socketErrorCategory
        receivedDatagram = $received
    }
}

function New-NormalDocument {
    param(
        [object]$Facts,
        [object]$Preflight,
        [string[]]$Order,
        [object[]]$ProbeResults
    )

    return [pscustomobject][ordered]@{
        schemaVersion = 1
        capturedAtUtc = [DateTimeOffset]::UtcNow.UtcDateTime.ToString('o')
        label = $Label
        inputs = New-InputProjection $LocalAddress $LocalPort $ServerAddress $ServerPort $TimeoutMilliseconds $script:InterfaceIndexRequested $InterfaceIndex $script:PacketInfoRequested
        route = $Facts.route
        interface = $Facts.interface
        localAddressMappings = $Facts.localAddressMappings
        udpEndpoints = $Facts.udpEndpoints
        probePreflight = $Preflight
        probeOrder = $Order
        probes = $ProbeResults
    }
}

function Write-NormalDocument {
    param([object]$Document)

    $json = ConvertTo-Json -InputObject $Document -Depth 10 -Compress
    [Console]::Out.WriteLine($json)
}

function Stop-WithMessage {
    param(
        [string]$Message,
        [int]$ExitCode = 1
    )

    [Console]::Error.WriteLine($Message)
    exit $ExitCode
}

try {
    if ($Mode -eq 'SelfTest') {
        $extraNames = @(
            'Label',
            'LocalAddress',
            'ServerAddress',
            'LocalPort',
            'ServerPort',
            'TimeoutMilliseconds',
            'InterfaceIndex',
            'UsePacketInfo',
            'AllowCredentialFreeChallenge'
        )
        foreach ($name in $extraNames) {
            if ($PSBoundParameters.ContainsKey($name)) {
                throw 'self_test_rejects_normal_mode_parameters'
            }
        }
        Invoke-SelfTest
        [Console]::Out.WriteLine('HOTSPOT_UDP_DIAGNOSTIC_SELF_TEST_OK')
        exit 0
    }

    if ($env:OS -ne 'Windows_NT') {
        throw 'windows_required'
    }
    if (-not $PSBoundParameters.ContainsKey('Label') -or
        $Label -notin @('hotspot-off', 'hotspot-on')) {
        throw 'label_must_be_hotspot-off_or_hotspot-on'
    }
    if (-not $PSBoundParameters.ContainsKey('LocalAddress') -or
        -not (Test-IPv4Literal $LocalAddress)) {
        throw 'local_address_must_be_ipv4_literal'
    }
    if (-not $PSBoundParameters.ContainsKey('ServerAddress') -or
        -not (Test-IPv4Literal $ServerAddress)) {
        throw 'server_address_must_be_ipv4_literal'
    }
    if (-not (Test-PortValue $LocalPort) -or -not (Test-PortValue $ServerPort)) {
        throw 'ports_must_be_1_to_65535'
    }
    if (-not (Test-TimeoutValue $TimeoutMilliseconds)) {
        throw 'timeout_must_be_100_to_30000'
    }
    if (-not (Test-InterfaceIndexModeCompatibility $Mode $script:InterfaceIndexRequested)) {
        throw 'interface_index_only_for_probe'
    }
    if ($script:InterfaceIndexRequested -and -not (Test-InterfaceIndexValue $InterfaceIndex)) {
        throw 'interface_index_must_be_1_to_16777215'
    }
    $packetInfoArgumentStatus = Get-PacketInfoArgumentStatus $Mode $script:PacketInfoRequested $script:InterfaceIndexRequested $LocalAddress
    if ($packetInfoArgumentStatus -ne 'valid') {
        throw $packetInfoArgumentStatus
    }
    if ($Mode -eq 'Snapshot' -and $AllowCredentialFreeChallenge) {
        throw 'snapshot_rejects_probe_confirmation'
    }

    foreach ($commandName in @(
        'Find-NetRoute',
        'Get-NetIPInterface',
        'Get-NetIPAddress',
        'Get-NetUDPEndpoint'
    )) {
        if ($null -eq (Get-Command $commandName -ErrorAction SilentlyContinue)) {
            throw "missing_prerequisite_$commandName"
        }
    }

    $facts = Get-SnapshotFacts $LocalAddress $ServerAddress $LocalPort
    if ($Mode -eq 'Snapshot') {
        $preflight = [pscustomobject][ordered]@{
            status = 'not_requested'
            owners = @()
        }
        Write-NormalDocument (New-NormalDocument $facts $preflight @() @())
        exit 0
    }

    if (-not $AllowCredentialFreeChallenge) {
        $preflight = [pscustomobject][ordered]@{
            status = 'confirmation_required'
            owners = @()
        }
        Write-NormalDocument (New-NormalDocument $facts $preflight @() @())
        [Console]::Error.WriteLine('probe_confirmation_required')
        exit 2
    }

    $preflight = Get-ProbePreflightDecision $facts.udpEndpoints
    if ($preflight.status -eq 'endpoint_query_failed') {
        Write-NormalDocument (New-NormalDocument $facts $preflight @() @())
        [Console]::Error.WriteLine('probe_endpoint_query_failed')
        exit 4
    }
    if ($preflight.status -eq 'local_port_in_use') {
        Write-NormalDocument (New-NormalDocument $facts $preflight @() @())
        [Console]::Error.WriteLine('probe_local_port_in_use')
        exit 3
    }
    if ($script:InterfaceIndexRequested -and $LocalAddress -eq '0.0.0.0') {
        $preflight = [pscustomobject][ordered]@{
            status = 'interface_index_requires_concrete_local_address'
            owners = @()
        }
        Write-NormalDocument (New-NormalDocument $facts $preflight @() @())
        [Console]::Error.WriteLine('probe_interface_index_requires_concrete_local_address')
        exit 5
    }
    if ($script:InterfaceIndexRequested) {
        $matchingMappings = @($facts.localAddressMappings | Where-Object {
            $null -ne $_.interfaceIndex -and [int]$_.interfaceIndex -eq $InterfaceIndex
        })
        if ($matchingMappings.Count -eq 0) {
            $preflight = [pscustomobject][ordered]@{
                status = 'interface_index_not_mapped_to_local_address'
                owners = @()
            }
            Write-NormalDocument (New-NormalDocument $facts $preflight @() @())
            [Console]::Error.WriteLine('probe_interface_index_not_mapped_to_local_address')
            exit 6
        }
    }
    if ($script:PacketInfoRequested) {
        Initialize-PacketInfoHelper
    }
    $localIPAddress = ConvertTo-IPv4Address $LocalAddress
    $serverIPAddress = ConvertTo-IPv4Address $ServerAddress
    $probeOrder = @('unconnected', 'connected')
    $probeResults = @()
    $requestedInterfaceIndex = if ($script:InterfaceIndexRequested) { $InterfaceIndex } else { $null }
    $probeResults += Invoke-ProbeAttempt 'unconnected' $localIPAddress $LocalPort $serverIPAddress $ServerPort $TimeoutMilliseconds $requestedInterfaceIndex $script:PacketInfoRequested
    $probeResults += Invoke-ProbeAttempt 'connected' $localIPAddress $LocalPort $serverIPAddress $ServerPort $TimeoutMilliseconds $requestedInterfaceIndex $script:PacketInfoRequested
    Write-NormalDocument (New-NormalDocument $facts $preflight $probeOrder $probeResults)
    exit 0
}
catch {
    $safeMessage = if ($_.Exception.Message -match '^[A-Za-z0-9_.-]+$') {
        $_.Exception.Message
    }
    else {
        'hotspot_udp_diagnostic_failed'
    }
    Stop-WithMessage $safeMessage
}
