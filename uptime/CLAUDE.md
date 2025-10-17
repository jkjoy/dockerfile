# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

This is a Flask-based microservice providing a web interface and API endpoints for system monitoring and domain information:
- `/` - Web interface with interactive dashboard
- `/uptime` - Returns host and container uptime/boot information (JSON)
- `/cert?host=example.com[:port]` - Returns SSL certificate expiration information for specified domains (JSON)
- `/whois?domain=example.com` - Returns domain WHOIS information (JSON)

The service runs in a Docker container using Python 3.13 Alpine and Gunicorn WSGI server. Features 1-day caching for certificate and WHOIS queries to reduce external API calls.

## Development Commands

### Build Docker Image
```bash
docker build -t uptime-cert:latest .
```

### Run with Docker Compose
```bash
docker-compose up -d
```

### Run Flask App Locally (Development)
```bash
pip install -r requirements.txt
python app.py
```

The service will be available at `http://localhost:5000`

### Run with Gunicorn (Production-like)
```bash
gunicorn -w 3 -b 0.0.0.0:5000 app:app
```

## Architecture

### Container Startup Flow
1. `entrypoint.sh` captures container start time by writing current timestamp to `/tmp/container_start_time`
2. Gunicorn launches with 3 workers serving the Flask app on port 5000

### Caching Mechanism
The service implements a simple in-memory caching system with TTL (Time To Live):
- **Cache Duration**: 24 hours (86400 seconds) for both certificate and WHOIS queries
- **Implementation**: `cache_with_ttl` decorator in app.py:26
- **Cache Key**: MD5 hash of function name + arguments
- **Purpose**: Reduce external API calls and improve response times for repeated queries

Both `get_cert_info()` and `get_whois_info()` functions are decorated with `@cache_with_ttl(ttl_seconds=86400)` to enable automatic caching.

### Uptime Detection Strategy
The `/uptime` endpoint provides two distinct uptime values:

- **host_uptime**: Detects the host machine boot time through multiple fallback methods:
  1. `/proc/uptime` (primary - works in most Linux containers)
  2. `uptime -s` command
  3. `who -b` command
  4. Windows `wmic` (fallback for Windows hosts)

  When running in a container, this typically reports the *host machine* uptime, not the container's.

- **container_uptime**: Reads the timestamp written by `entrypoint.sh` to `/tmp/container_start_time` to provide accurate container start time and runtime duration.

### Certificate Validation
The `/cert` endpoint (`get_cert_info` function in app.py:218):
- Establishes SSL/TLS connection to the target host:port
- Extracts certificate details using `cryptography` and `x509` libraries
- Returns parsed certificate information including:
  - Validity periods (not_before, not_after)
  - Time remaining until expiration (seconds and days)
  - Issuer and subject information
  - Subject Alternative Names (SAN)
- Results are cached for 24 hours

### WHOIS Query
The `/whois` endpoint (`get_whois_info` function in app.py:149):
- Uses `python-whois` library to query domain registration information
- Automatically cleans domain input (removes http://, https://, ports, paths)
- Returns domain information including:
  - Registrar information
  - Creation, expiration, and update dates
  - Days until expiration (automatically calculated)
  - Name servers and domain status
- Results are cached for 24 hours

### Web Interface
The service includes a responsive web dashboard (`templates/index.html`):
- **Auto-refreshing uptime display**: Shows host and container uptime, refreshes every 60 seconds
- **Certificate query form**: Interactive SSL certificate checker with color-coded expiration warnings
- **WHOIS query form**: Domain information lookup with formatted JSON results
- **Responsive design**: Purple gradient theme, mobile-friendly layout

### API Endpoints

**GET /**
Web interface with interactive dashboard for all monitoring features.

**GET /uptime**
Returns JSON with host and container boot times, uptime durations, and timestamps.

**GET /cert?host=example.com**
Query parameters:
- `host` - Single domain (example.com or example.com:8443)
- `host` - Multiple values supported (multiple `&host=` params)
- `host` - Comma-separated domains (example.com,foo.com)
- `timeout` - Optional connection timeout in seconds (default: 5.0)

**GET /health**
Health check endpoint returning `{"status": "ok", "time": timestamp}`

**GET /whois?domain=example.com**
Query parameters:
- `domain` - Single domain (example.com)
- `domain` - Multiple values supported (multiple `&domain=` params or comma-separated)

Returns domain registration information including registrar, creation/expiration dates, name servers, and domain status.

## File Structure

- `app.py` - Main Flask application with all endpoint logic and caching
- `templates/index.html` - Responsive web dashboard with JavaScript for real-time updates
- `entrypoint.sh` - Container initialization script
- `requirements.txt` - Python dependencies
- `Dockerfile` - Multi-stage build configuration
- `docker-compose.yml` - Container orchestration setup

## Key Dependencies

- Flask: Web framework
- Gunicorn: WSGI production server
- cryptography: Certificate parsing and validation
- python-whois: Domain WHOIS information lookup

## Important Notes

- The cache is in-memory only and will be cleared on container restart
- WHOIS queries may fail for some domains due to rate limiting or unsupported TLDs
- Certificate validation requires network access to the target host on the specified port
