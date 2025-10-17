#!/usr/bin/env python3
# -*- coding: utf-8 -*-

from flask import Flask, jsonify, request, render_template
import os
import time
import platform
import subprocess
import socket
import ssl
import json
from datetime import datetime, timedelta, timezone
from cryptography import x509
from cryptography.hazmat.backends import default_backend
from functools import wraps
import hashlib

try:
    import whois
except ImportError:
    whois = None

app = Flask(__name__)

# -------------------------
# Cache mechanism (1 day TTL)
# -------------------------
_cache = {}

def cache_with_ttl(ttl_seconds=86400):
    """缓存装饰器，默认1天过期"""
    def decorator(func):
        @wraps(func)
        def wrapper(*args, **kwargs):
            # 生成缓存键
            cache_key = hashlib.md5(
                f"{func.__name__}:{str(args)}:{str(kwargs)}".encode()
            ).hexdigest()

            # 检查缓存
            if cache_key in _cache:
                cached_data, cached_time = _cache[cache_key]
                if time.time() - cached_time < ttl_seconds:
                    return cached_data

            # 执行函数并缓存结果
            result = func(*args, **kwargs)
            _cache[cache_key] = (result, time.time())
            return result
        return wrapper
    return decorator

# -------------------------
# Boot / uptime helpers
# -------------------------
def get_host_boot_time():
    """尝试获取宿主机/内核的开机时间（若在容器内，这通常是宿主机的uptime）。返回 timestamp 或 None。"""
    system = platform.system().lower()
    # 1) /proc/uptime -> uptime seconds
    if system == "linux" and os.path.isfile("/proc/uptime"):
        try:
            with open("/proc/uptime", "r") as f:
                uptime_seconds = float(f.readline().split()[0])
            boot_time = time.time() - uptime_seconds
            return int(boot_time)
        except Exception:
            pass

    # 2) uptime -s
    try:
        out = subprocess.check_output(["uptime", "-s"], text=True).strip()
        if out:
            for fmt in ("%Y-%m-%d %H:%M:%S", "%Y-%m-%d %H:%M"):
                try:
                    dt = datetime.strptime(out, fmt)
                    return int(dt.timestamp())
                except Exception:
                    pass
    except Exception:
        pass

    # 3) who -b
    try:
        out = subprocess.check_output(["who", "-b"], text=True).strip()
        import re
        m = re.search(r'(\d{4}-\d{2}-\d{2})\s+(\d{2}:\d{2}(?::\d{2})?)', out)
        if m:
            s = f"{m.group(1)} {m.group(2)}"
            for fmt in ("%Y-%m-%d %H:%M:%S", "%Y-%m-%d %H:%M"):
                try:
                    dt = datetime.strptime(s, fmt)
                    return int(dt.timestamp())
                except Exception:
                    pass
    except Exception:
        pass

    # 4) Windows (unlikely in docker)
    if system.startswith("win"):
        try:
            out = subprocess.check_output(["wmic", "os", "get", "lastbootuptime"], text=True)
            for line in out.splitlines():
                line = line.strip()
                if line and line[0].isdigit():
                    s = line.split('.')[0]
                    dt = datetime.strptime(s, "%Y%m%d%H%M%S")
                    return int(dt.timestamp())
        except Exception:
            pass

    return None

def get_container_start_time_file(path="/tmp/container_start_time"):
    """如果 entrypoint 在容器启动时写入了时间戳文件，则读取它作为容器启动时间（秒级 timestamp）"""
    try:
        if os.path.isfile(path):
            with open(path, "r") as f:
                s = f.read().strip()
            if s:
                try:
                    return int(float(s))
                except Exception:
                    pass
    except Exception:
        pass
    return None

def format_uptime_info(boot_ts):
    now_ts = int(time.time())
    if boot_ts is None:
        return {
            "boot_time": None,
            "boot_time_str": None,
            "uptime_seconds": None,
            "uptime_str": None
        }
    uptime_sec = now_ts - boot_ts
    return {
        "boot_time": int(boot_ts),
        "boot_time_str": datetime.fromtimestamp(boot_ts).strftime("%Y-%m-%d %H:%M:%S"),
        "uptime_seconds": int(uptime_sec),
        "uptime_str": str(timedelta(seconds=int(uptime_sec)))
    }

# -------------------------
# WHOIS query helper
# -------------------------

@cache_with_ttl(ttl_seconds=86400)  # 缓存1天
def get_whois_info(domain: str) -> dict:
    """查询域名WHOIS信息"""
    result = {
        "domain": domain,
        "success": False,
        "error": None,
        "registrar": None,
        "creation_date": None,
        "expiration_date": None,
        "updated_date": None,
        "name_servers": [],
        "status": [],
        "whois_server": None,
        "raw": None
    }

    if whois is None:
        result["error"] = "WHOIS module not available. Please install python-whois package."
        return result

    try:
        # 清理域名，移除协议和路径
        domain = domain.replace("http://", "").replace("https://", "").split("/")[0].split(":")[0]

        w = whois.whois(domain)

        # 处理日期（可能是单个日期或日期列表）
        def format_date(date_value):
            if date_value is None:
                return None
            if isinstance(date_value, list):
                date_value = date_value[0] if date_value else None
            if isinstance(date_value, datetime):
                return date_value.isoformat()
            return str(date_value)

        result.update({
            "success": True,
            "registrar": w.registrar if hasattr(w, 'registrar') else None,
            "creation_date": format_date(w.creation_date) if hasattr(w, 'creation_date') else None,
            "expiration_date": format_date(w.expiration_date) if hasattr(w, 'expiration_date') else None,
            "updated_date": format_date(w.updated_date) if hasattr(w, 'updated_date') else None,
            "name_servers": w.name_servers if isinstance(w.name_servers, list) else [w.name_servers] if hasattr(w, 'name_servers') and w.name_servers else [],
            "status": w.status if isinstance(w.status, list) else [w.status] if hasattr(w, 'status') and w.status else [],
            "whois_server": w.whois_server if hasattr(w, 'whois_server') else None,
        })

        # 计算过期剩余时间
        if hasattr(w, 'expiration_date') and w.expiration_date:
            exp_date = w.expiration_date
            if isinstance(exp_date, list):
                exp_date = exp_date[0]
            if isinstance(exp_date, datetime):
                now = datetime.now()
                if exp_date.tzinfo is not None:
                    now = datetime.now(timezone.utc)
                days_remaining = (exp_date - now).total_seconds() / 86400
                result["days_until_expiration"] = round(days_remaining, 2)

    except Exception as e:
        result["error"] = str(e)

    return result

# -------------------------
# Certificate check helper (based on previous implementation)
# -------------------------

@cache_with_ttl(ttl_seconds=86400)  # 缓存1天
def get_cert_info(host: str, port: int = 443, timeout: float = 5.0) -> dict:
    result = {
        "host_requested": host,
        "port": port,
        "success": False,
        "error": None,
        "not_before": None,
        "not_after": None,
        "not_after_ts": None,
        "seconds_remaining": None,
        "days_remaining": None,
        "issuer": None,
        "subject": None,
        "san": []
    }

    try:
        ctx = ssl.create_default_context()
        ctx.check_hostname = True
        ctx.verify_mode = ssl.CERT_REQUIRED

        with socket.create_connection((host, port), timeout=timeout) as sock:
            with ctx.wrap_socket(sock, server_hostname=host) as ssock:
                der_cert = ssock.getpeercert(binary_form=True)
                cert = x509.load_der_x509_certificate(der_cert, default_backend())

                not_before = cert.not_valid_before.replace(tzinfo=timezone.utc)
                not_after = cert.not_valid_after.replace(tzinfo=timezone.utc)
                now = datetime.now(timezone.utc)

                seconds_remaining = int((not_after - now).total_seconds())
                days_remaining = seconds_remaining / 86400.0

                # issuer / subject
                issuer = ", ".join([f"{name.oid._name}={name.value}" for name in cert.issuer])
                subject = ", ".join([f"{name.oid._name}={name.value}" for name in cert.subject])

                # SAN
                san = []
                try:
                    ext = cert.extensions.get_extension_for_class(x509.SubjectAlternativeName)
                    san = ext.value.get_values_for_type(x509.DNSName)
                except Exception:
                    pass

                result.update({
                    "success": True,
                    "not_before": not_before.isoformat(),
                    "not_after": not_after.isoformat(),
                    "not_after_ts": int(not_after.timestamp()),
                    "seconds_remaining": seconds_remaining,
                    "days_remaining": round(days_remaining, 6),
                    "issuer": issuer,
                    "subject": subject,
                    "san": san
                })
    except Exception as e:
        result["error"] = str(e)

    return result


# -------------------------
# Flask endpoints
# -------------------------
@app.route("/", methods=["GET"])
def index():
    """首页"""
    return render_template("index.html")

@app.route("/uptime", methods=["GET"])
def uptime_endpoint():
    """
    返回 JSON：
      - host_uptime: 从 /proc 或 uptime/who 获取的 '宿主机/内核' 开机信息（在容器通常是宿主机）
      - container_uptime: 如果容器 entrypoint 写入了 /tmp/container_start_time，则返回容器启动时间与运行时长
    """
    host_boot_ts = get_host_boot_time()
    host_info = format_uptime_info(host_boot_ts)

    container_boot_ts = get_container_start_time_file("/tmp/container_start_time")
    container_info = format_uptime_info(container_boot_ts)

    # 如果没有容器文件，也尝试用 process start time (as fallback)
    if container_boot_ts is None:
        # 获取当前进程启动时间（仅近似容器内程序启动时间）
        try:
            # proc stat on linux: /proc/1 (PID 1 inside container) may be our entrypoint if running in container
            pid1_stat = None
            if os.path.exists("/proc/1/stat"):
                with open("/proc/1/stat", "r") as f:
                    pid1_stat = f.read().split()
            if pid1_stat:
                # field 22 is starttime (in clock ticks) since boot; need system uptime and Hertz
                # This gets complicated and is approximate — 所以仅作为 fallback，不保证准确
                container_info["note"] = "容器启动时间未写入文件；未提供精确容器启动时间，仅返回 null（若需要容器启动时间，请在 Docker entrypoint 写入 /tmp/container_start_time）。"
        except Exception:
            pass

    response = {
        "host_uptime": host_info,
        "container_uptime": container_info,
        "generated_at": int(time.time())
    }
    return jsonify(response)

@app.route("/cert", methods=["GET"])
def cert_endpoint():
    """
    /cert?host=example.com             -> check example.com:443
    /cert?host=example.com:8443       -> check specific port
    /cert?host=example.com,foo.com    -> comma separated
    /cert?host=example.com&host=foo.com -> multiple params
    """
    hosts = request.args.getlist("host")
    if not hosts:
        raw = request.args.get("host")
        if raw:
            hosts = [h.strip() for h in raw.split(",") if h.strip()]
    if not hosts:
        return jsonify({"error": "请提供 host 参数，例如 /cert?host=example.com 或 /cert?host=example.com:8443"}), 400

    timeout = float(request.args.get("timeout", 5.0))
    results = []
    for h in hosts:
        if ":" in h and h.count(":") == 1 and not h.startswith("["):
            hostname, port_s = h.split(":")
            try:
                port = int(port_s)
            except Exception:
                port = 443
        else:
            hostname = h
            port = 443
        results.append(get_cert_info(hostname, port=port, timeout=timeout))

    return jsonify(results if len(results) > 1 else results[0])

@app.route("/health", methods=["GET"])
def health():
    return jsonify({"status": "ok", "time": int(time.time())})

@app.route("/whois", methods=["GET"])
def whois_endpoint():
    """
    /whois?domain=example.com -> 查询单个域名
    /whois?domain=example.com,foo.com -> 查询多个域名（逗号分隔）
    /whois?domain=example.com&domain=foo.com -> 查询多个域名（多个参数）
    """
    domains = request.args.getlist("domain")
    if not domains:
        raw = request.args.get("domain")
        if raw:
            domains = [d.strip() for d in raw.split(",") if d.strip()]
    if not domains:
        return jsonify({"error": "请提供 domain 参数，例如 /whois?domain=example.com"}), 400

    results = []
    for domain in domains:
        results.append(get_whois_info(domain))

    return jsonify(results if len(results) > 1 else results[0])

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000, debug=False)
