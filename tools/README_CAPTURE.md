# RTMP Network Traffic Capture Tools

This directory contains scripts for capturing and analyzing network traffic during RTMP multi-destination relay testing.

## Scripts

### 1. `capture-rtmp-traffic.sh`
Captures all TCP traffic on RTMP ports (1935, 1936, 1937) and converts to human-readable format.

**Usage:**
```bash
./capture-rtmp-traffic.sh [duration_seconds]
```

**Features:**
- Captures network traffic using `tcpdump`
- Monitors ports 1935, 1936, 1937 (RTMP relay and destination servers)
- Converts PCAP to ASCII format using `tshark`
- Creates summary files with connection analysis
- Real-time packet counting during capture

**Requirements:**
- `tcpdump` (install with `brew install tcpdump`)
- `tshark` (install with `brew install wireshark`)
- Root privileges or tcpdump permissions

### 2. `run-test-with-capture.sh`
Combined script that runs both traffic capture and RTMP test simultaneously.

**Usage:**
```bash
./run-test-with-capture.sh [duration_seconds]
```

**Features:**
- Starts network capture first
- Launches RTMP multi-destination relay test
- Monitors both processes
- Provides comprehensive results summary

### 3. `test-multi-destination-relay.sh`
Original RTMP test script (existing).

## Network Topology

The scripts monitor this traffic flow:
```
FFmpeg → Relay Server (1935) → RTMP Server 1 (1936)
                             → RTMP Server 2 (1937)
```

## Output Files

### Capture Files
- `captures/rtmp_traffic_YYYYMMDD_HHMMSS.pcap` - Raw packet capture
- `captures/rtmp_traffic_YYYYMMDD_HHMMSS.txt` - Full ASCII dump
- `captures/rtmp_traffic_YYYYMMDD_HHMMSS_summary.txt` - Connection summary

### Log Files
- `logs/tcpdump_YYYYMMDD_HHMMSS.log` - tcpdump output
- `logs/capture_output.log` - Capture script output
- `logs/test_output.log` - RTMP test output

## Quick Start

1. **Simple capture during manual test:**
   ```bash
   # Terminal 1: Start capture
   ./capture-rtmp-traffic.sh 60
   
   # Terminal 2: Start your RTMP test
   ./test-multi-destination-relay.sh
   ```

2. **Automated combined test:**
   ```bash
   ./run-test-with-capture.sh 120
   ```

3. **Analyze captured traffic:**
   ```bash
   # View with tcpdump
   tcpdump -r captures/rtmp_traffic_*.pcap -nn -v
   
   # View with Wireshark GUI
   wireshark captures/rtmp_traffic_*.pcap
   
   # View summary
   less captures/rtmp_traffic_*_summary.txt
   ```

## Permissions Setup

If you get permission errors, set up tcpdump permissions:

```bash
# Option 1: Run with sudo
sudo ./capture-rtmp-traffic.sh

# Option 2: Set tcpdump permissions (one time setup)
sudo chown root $(which tcpdump)
sudo chmod +s $(which tcpdump)
```

## Troubleshooting

### No packets captured
- Check that RTMP servers are running on expected ports
- Verify FFmpeg is actually connecting
- Try capturing on different interface (`-i any` instead of `-i lo0`)

### Permission denied
- Run with `sudo` or set tcpdump permissions (see above)
- Check that tcpdump is installed: `brew install tcpdump`

### ASCII conversion failed
- Install Wireshark for tshark: `brew install wireshark`
- Check that PCAP file contains data

### Analysis tips
- Look for TCP handshakes on ports 1935, 1936, 1937
- RTMP handshake should show C0/C1/S0/S1/S2/C2 sequence
- Monitor for connection establishment and data flow patterns
- Check for any connection failures or retransmissions

## Example Analysis Workflow

1. Run combined test:
   ```bash
   ./run-test-with-capture.sh 60
   ```

2. Check test success:
   ```bash
   tail -20 logs/test_output.log
   ```

3. Quick traffic summary:
   ```bash
   less captures/rtmp_traffic_*_summary.txt
   ```

4. Detailed analysis:
   ```bash
   wireshark captures/rtmp_traffic_*.pcap
   ```

5. Look for RTMP handshake patterns:
   ```bash
   tcpdump -r captures/rtmp_traffic_*.pcap -A | grep -A5 -B5 "RTMP"
   ```