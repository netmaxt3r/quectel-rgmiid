# Quectel RGMII Web Control Panel & Daemon

A lightweight Go daemon and web-based dashboard designed to connect to the Quectel RGMII AT command TCP interface, poll system status, and expose commands and diagnostic endpoints.

## Features

- **Web Dashboard**: Simple HTML/CSS/JS frontend showing signal info, cell details, and system status.
- **Basic Authentication**: Lock down endpoints using credentials.
- **AT Command Console**: Run raw AT commands on the modem directly from the web panel, or programmatically via a JSON endpoint.
- **Diagnostic Endpoint**: Optional `/api/debug` diagnostic status endpoint.

---

## Configuration Options

Options can be set via CLI flags or mapped to environment variables. CLI flags always take precedence.

| CLI Flag | Env Variable | Default Fallback | Description |
| :--- | :--- | :--- | :--- |
| `-modem` | `MODEM_ADDR` | `"192.168.225.1:1555"` | Modem TCP AT interface address (`IP:port`) |
| `-port` | `PORT` | `"8080"` | Port to bind the web server to |
| `-interval` | `POLL_INTERVAL` | `5` | Background stats polling interval in seconds |
| `-user` | `AUTH_USER` | `""` (disabled) | Web Session Auth Username |
| `-pass` | `AUTH_PASS` | `""` (disabled) | Web Session Auth Password |
| `-key` | `AUTH_KEY` | `""` (disabled) | Static API Key for external tools (e.g. scripts/curl) |
| `-mqtt-server` | `MQTT_SERVER` | `""` (disabled) | MQTT Broker Server URL (e.g., `tcp://10.24.23.6:1883`) |
| `-mqtt-user` | `MQTT_USER` | `""` | MQTT connection username |
| `-mqtt-pass` | `MQTT_PASS` | `""` | MQTT connection password |
| `-mqtt-topic` | `MQTT_TOPIC` | `"rgmii"` | MQTT base topic for status updates |
| `-mqtt-discovery` | `MQTT_DISCOVERY` | `true` | Enable Home Assistant MQTT Auto Discovery |
| `-mqtt-discovery-prefix` | `MQTT_DISCOVERY_PREFIX` | `"homeassistant"` | Home Assistant discovery prefix |
| N/A | `QUECTEL_DEBUG` | `""` (disabled) | Set to `1` to enable the `/api/debug` JSON endpoint |

---

## MQTT & Home Assistant Integration

When MQTT is activated by specifying the `-mqtt-server` URL (or `MQTT_SERVER` env variable), the daemon publishes status updates and registers itself with Home Assistant:

* **State Topic:** Updates containing the entire modem status JSON are published to `<MQTT_TOPIC>/status` (e.g. `rgmii/status`).
* **Home Assistant Auto Discovery:** The daemon automatically publishes configuration payloads to registers 18 entities (signal strength percentage/CSQ, carrier name, registration state, connection speed details, LTE and 5G cellular metrics, SIM card number) under a single device named **Quectel RGMII Modem**.


---

## Standalone Executable

### Prerequisites

- [Go 1.26+](https://go.dev/) installed on your machine.
- [Node.js](https://nodejs.org/) and Yarn (configured with Corepack) to compile stylesheets.

### Build

1. **Compile Tailwind CSS Stylesheet:**
   Compile the optimized and minified Tailwind CSS stylesheet:
   ```bash
   corepack enable
   yarn install
   yarn build:css
   ```

2. **Compile the Go Daemon Binary:**
   Compile the final standalone daemon binary (which embeds all static assets):
   ```bash
   go build -o rgmii_daemon .
   ```

### Run

Run with default options:

```bash
./rgmii_daemon
```

Or run with customized flags:

```bash
./rgmii_daemon -port 9090 -user admin -pass secret -modem 192.168.1.1:1555
```

Or run using environment variables:

```bash
export PORT=9090
export AUTH_USER=admin
export AUTH_PASS=secret
./rgmii_daemon
```

---

## Linux Container

Pre-built Docker/Podman container images are available on the GitHub Container Registry:
`ghcr.io/netmaxt3r/quectel-rgmiid:latest`

### Run Pre-built Image

Run the container, binding port 8080:

```bash
docker run -d \
  -p 8080:8080 \
  --name rgmii_control \
  ghcr.io/netmaxt3r/quectel-rgmiid:latest
```

Pass customized arguments or environment variables to the container:

```bash
docker run -d \
  -p 9090:9090 \
  -e PORT=9090 \
  -e AUTH_USER=admin \
  -e AUTH_PASS=secret \
  -e MODEM_ADDR=192.168.1.1:1555 \
  --name rgmii_control \
  ghcr.io/netmaxt3r/quectel-rgmiid:latest
```

### Build Image Locally

If you prefer to build the image locally from the root of the project repository:

```bash
docker build -t quectel-rgmiid:latest .
```
