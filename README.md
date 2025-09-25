# YAMN Tor Integration

Enhanced version of [YAMN (Yet Another Mixmaster Network)](https://github.com/crooks/yamn) with native Tor network integration for improved anonymity and censorship resistance.

## Project Overview

We have developed an enhanced version of YAMN (Yet Another Mixmaster Network) with native Tor network integration. This project significantly improves the anonymity and security of the mixmaster remailer network by leveraging Tor's onion routing capabilities.

**Mission:** To create a more secure, anonymous, and censorship-resistant email remailer system by seamlessly integrating Tor network capabilities into the existing YAMN infrastructure.

### Key Benefits

- Enhanced anonymity through Tor's multi-layer encryption
- Resistance to traffic analysis and surveillance
- Support for .onion hidden services
- Improved censorship circumvention

## Implementation Status

### Completed Features

- Transparent SOCKS5 proxy integration for Tor routing
- Automatic .onion address detection and handling
- Circuit refresh mechanism for enhanced security
- Tor connectivity validation and health checks
- Enhanced SMTP relay with Tor support
- Configuration system for Tor parameters

### Example Usage

```bash
# First create a message file with proper email headers
cat > message.txt << 'EOF'
To: mail2news@xilb7y4kj6u6qfo45o3yk2kilfv54ffukzei3puonuqlncy7cn2afwyd.onion
From: Anonymous User <anon@example.com>
Subject: Test message via Tor
Newsgroups: alt.test
Date: $(date -R)

This is a test message sent through YAMN with Tor integration.
The message will be routed through the Tor network for enhanced anonymity.

Best regards,
Anonymous
EOF

# Send the message
./yamn --client mail2news@xilb7y4kj6u6qfo45o3yk2kilfv54ffukzei3puonuqlncy7cn2afwyd.onion message.txt

# Automatic Tor detection for .onion addresses
# Transparent routing through Tor SOCKS proxy
# Circuit refresh every 10 minutes
```

## Technical Implementation

### Modified Architecture

- Modified SMTP relay functions for Tor compatibility
- Enhanced configuration system with Tor parameters
- Circuit management and refresh scheduling
- Stream isolation for maximum anonymity

### Dependencies Added

- `golang.org/x/net v0.10.0` (for SOCKS5 proxy support)

### Core Integration

```go
// Core Tor integration
func dialThroughTor(address string) (net.Conn, error) {
    proxyURL, _ := url.Parse("socks5://127.0.0.1:9050")
    dialer, _ := proxy.FromURL(proxyURL, proxy.Direct)
    return dialer.Dial("tcp", address)
}
```

## Installation Instructions

### 1. Get Original YAMN Source Code

First, clone the original YAMN repository:

```bash
git clone https://github.com/crooks/yamn.git
cd yamn
```

### 2. Download Modified Files

Download and replace these files with our Tor-enhanced versions:

- **yamn.go** → Replace existing main file
- **mail.go** → Replace existing mail handling functions
- **config.go** → Replace config/config.go file
- **yamn.yml** → Create new configuration file in root directory

*Note: Place config.go in the config/ subdirectory of your YAMN installation*

### 3. System Prerequisites

Install and configure Tor on your system:

```bash
# Install Tor
sudo apt update && sudo apt install tor

# Configure Tor (/etc/tor/torrc)
echo "SocksPort 9050" | sudo tee -a /etc/tor/torrc

# Start and enable Tor service
sudo systemctl start tor
sudo systemctl enable tor

# Verify Tor is running
sudo netstat -tlnp | grep :9050
```

### 4. Build with Security Hardening

Compile YAMN with security-focused build parameters:

```bash
# Add new dependency
go mod tidy

# Build with security hardening flags
go build -ldflags="-s -w -X main.version=0.2.8-tor" \
         -trimpath \
         -buildmode=pie \
         -o yamn .

# Set proper permissions
chmod 755 yamn
```

**Build flags explained:**
- `-ldflags="-s -w"` → Strip debugging symbols for smaller binary
- `-trimpath` → Remove local path information from binary
- `-buildmode=pie` → Position Independent Executable for ASLR

### 5. Server Deployment for .onion Reachability

Configure your server to enable final destination servers to reach Tor hidden services:

```bash
# Create system directories
sudo mkdir -p /etc/yamn /var/spool/yamn /var/log/yamn /var/lib/yamn

# Copy configuration and set ownership
sudo cp yamn.yml /etc/yamn/
sudo chown -R yamn:yamn /var/spool/yamn /var/log/yamn /var/lib/yamn
sudo chmod 700 /var/spool/yamn /var/lib/yamn

# Install as system service (optional)
sudo cp yamn /usr/local/bin/
```

### 6. Configuration for Production

Edit `/etc/yamn/yamn.yml` for production deployment:

```yaml
# Essential Tor configuration
tor:
  enabled: true
  required: true          # Exit if Tor unavailable
  socksproxy: "127.0.0.1:9050"
  circuit_reset: 10

# Mail configuration for .onion reachability  
mail:
  force_tor_smtp: false   # Allow both Tor and direct
  onion_relay: true       # Enable .onion MX handling
  disable_tls_onion: true # TLS redundant over Tor

# Production settings
remailer:
  daemon: true
  exit: true              # Enable final delivery
```

### 7. Testing and Verification

```bash
# Test configuration
./yamn --config /etc/yamn/yamn.yml --debug

# Test Tor connectivity
curl --socks5 127.0.0.1:9050 https://check.torproject.org/

# Test .onion message delivery
./yamn --config /etc/yamn/yamn.yml --client \
       mail2news@xilb7y4kj6u6qfo45o3yk2kilfv54ffukzei3puonuqlncy7cn2afwyd.onion message.txt

# Run as daemon
./yamn --config /etc/yamn/yamn.yml --remailer --daemon
```

## Security Enhancements

### Privacy Protection

- All outbound connections routed through Tor
- Automatic DNS leak prevention
- Circuit rotation to prevent correlation attacks
- Stream isolation for maximum anonymity

### Threat Model

- Protection against IP address correlation
- Resistance to traffic analysis attacks
- Mitigation of timing correlation attacks
- Enhanced protection in censored environments

## Future Development

### Planned Features

- Optional memguard integration for memory protection
- Performance optimizations
- Cross-platform compatibility improvements
- Enhanced configuration validation

**Next Phase:** We are exploring the integration of memguard for enhanced memory protection, potentially as a modular plugin to secure sensitive message data in RAM.

## Configuration Reference

Basic Tor configuration in `yamn.yml`:

```yaml
# Configurazione generale
general:
  loglevel: "INFO"
  logtofile: true
  logtojournal: false

# Configurazione Tor (semplificata)
tor:
  enabled: true
  required: false
  socksproxy: "127.0.0.1:9050"
  timeout: 30
  circuit_reset: 10

# Configurazione mail
mail:
  sendmail: false
  usetls: true
  smtp_relay: "localhost"
  smtp_port: 25
  mx_relay: true
  onion_relay: true
  outbound_name: "Anonymous Tor Remailer"
  force_tor_smtp: false
  disable_tls_onion: true

# Configurazione remailer
remailer:
  name: "Simple Tor Remailer"
  exit: true
  daemon: true
```

## Privacy Through Technology

This implementation provides transport-layer anonymity in addition to the existing Mixmaster protocol anonymity. By integrating Tor capabilities into the proven Mixmaster protocol, we're building more robust tools for anonymous communication.

**Use responsibly and in accordance with your local laws.**

## License

This project maintains the same license as the original YAMN project. Please refer to the [upstream repository](https://github.com/crooks/yamn) for license details.

## Acknowledgments

- Original YAMN project by Steve Crook
- Tor Project for the anonymous networking protocol  
- Mixmaster community for the underlying remailer protocol
