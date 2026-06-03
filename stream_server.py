#!/usr/bin/env python3
"""
DashBox Camera Stream Server — WebSocket pub/sub relay with bidirectional control.

Device: /ws/device?token=TOKEN&device_id=ID
  - Sends raw JPEG frames as binary messages.
  - Receives control commands (stream_start, stream_stop, switch_camera).

Viewer (App): /ws/viewer
  - Subscribes to device frames via {"method":"subscribe","device_id":"..."}
  - Sends control commands forwarded to device.
  - Receives binary JPEG frames + text status messages.
"""

import asyncio
import json
import argparse
import sys
from datetime import datetime

import websockets
from websockets.asyncio.server import ServerConnection

# ── Config ─────────────────────────────────────────
STREAM_TOKEN = "dashbox-stream-2026"
DEVICE_TOKENS = {
    "ab93973abb2ebbc9": "dashbox-stream-2026",
}

# ── State ──────────────────────────────────────────
devices: dict[str, ServerConnection] = {}           # device_id → device WS
subscribers: dict[str, set[ServerConnection]] = {}   # device_id → set[viewer WS]
viewer_device: dict[ServerConnection, str] = {}      # viewer WS → subscribed device_id

def log(msg: str) -> None:
    ts = datetime.now().strftime("%H:%M:%S")
    print(f"[{ts}] {msg}", flush=True)


# ── Device handler ─────────────────────────────────
async def handle_device(ws: ServerConnection, device_id: str) -> None:
    devices[device_id] = ws
    log(f"DEVICE ONLINE: {device_id}")
    # Notify subscribers
    await _notify_subscribers(device_id, {"type": "device_online", "device_id": device_id})

    try:
        async for message in ws:
            if isinstance(message, bytes):
                # JPEG frame → forward to all subscribers
                await _broadcast_frame(device_id, message)
            else:
                # Text response from device (e.g., stream_started ack)
                try:
                    msg = json.loads(message)
                    # Forward device responses to its subscribers
                    await _notify_subscribers(device_id, msg)
                except json.JSONDecodeError:
                    pass
    except websockets.ConnectionClosed:
        pass
    finally:
        devices.pop(device_id, None)
        await _notify_subscribers(device_id, {"type": "device_offline", "device_id": device_id})
        # Clean up subscribers for this device
        subs = subscribers.pop(device_id, set())
        for sub in subs:
            viewer_device.pop(sub, None)
            try:
                await sub.send(json.dumps({"type": "stream_end", "device_id": device_id}))
            except websockets.ConnectionClosed:
                pass
        log(f"DEVICE OFFLINE: {device_id}")


async def _broadcast_frame(device_id: str, frame: bytes) -> None:
    """Send binary JPEG frame to all subscribers of device_id."""
    subs = subscribers.get(device_id, set())
    if not subs:
        return
    dead = set()
    for sub in subs:
        try:
            await sub.send(frame)
        except websockets.ConnectionClosed:
            dead.add(sub)
    subs.difference_update(dead)


async def _notify_subscribers(device_id: str, msg: dict) -> None:
    """Send JSON message to all subscribers of device_id."""
    subs = subscribers.get(device_id, set())
    if not subs:
        return
    raw = json.dumps(msg)
    dead = set()
    for sub in subs:
        try:
            await sub.send(raw)
        except websockets.ConnectionClosed:
            dead.add(sub)
    subs.difference_update(dead)


# ── Viewer handler ─────────────────────────────────
async def handle_viewer(ws: ServerConnection) -> None:
    device_id = None
    try:
        async for message in ws:
            if isinstance(message, bytes):
                continue  # viewers don't send binary

            try:
                msg = json.loads(message)
            except json.JSONDecodeError:
                continue

            method = msg.get("method", "")

            if method == "subscribe":
                new_id = msg.get("device_id", "")
                # Unsubscribe old
                if device_id and device_id in subscribers:
                    subscribers[device_id].discard(ws)
                    viewer_device.pop(ws, None)
                # Subscribe new
                device_id = new_id
                if device_id:
                    subscribers.setdefault(device_id, set()).add(ws)
                    viewer_device[ws] = device_id
                    online = device_id in devices
                    await ws.send(json.dumps({
                        "type": "subscribed",
                        "device_id": device_id,
                        "online": online,
                    }))
                    log(f"VIEWER subscribed to {device_id} (online={online})")

            elif method == "unsubscribe":
                if device_id and device_id in subscribers:
                    subscribers[device_id].discard(ws)
                    viewer_device.pop(ws, None)
                device_id = None
                await ws.send(json.dumps({"type": "unsubscribed"}))

            elif method in ("stream_start", "stream_stop", "switch_camera"):
                # Forward control command to device
                target_id = msg.get("device_id", device_id)
                if not target_id:
                    await ws.send(json.dumps({
                        "type": "error", "message": "no device_id specified"
                    }))
                    continue
                dev_ws = devices.get(target_id)
                if not dev_ws:
                    await ws.send(json.dumps({
                        "type": "error", "message": f"device {target_id} not connected"
                    }))
                    continue
                # Forward to device
                try:
                    await dev_ws.send(json.dumps(msg))
                    log(f"CTRL → DEVICE {target_id}: {method}")
                except websockets.ConnectionClosed:
                    await ws.send(json.dumps({
                        "type": "error", "message": "device disconnected"
                    }))

            elif method == "ping":
                await ws.send(json.dumps({"method": "pong"}))

    except websockets.ConnectionClosed:
        pass
    finally:
        if device_id and device_id in subscribers:
            subscribers[device_id].discard(ws)
        viewer_device.pop(ws, None)
        log(f"VIEWER OFFLINE (was watching: {device_id})")


# ── Auth ───────────────────────────────────────────
def verify_device_token(device_id: str, token: str) -> bool:
    if token == STREAM_TOKEN:
        return True
    return DEVICE_TOKENS.get(device_id) == token


# ── Router ─────────────────────────────────────────
async def router(ws: ServerConnection) -> None:
    path = ws.request.path if hasattr(ws, 'request') else "/"

    if path.startswith("/ws/device"):
        params = {}
        if "?" in path:
            query = path.split("?", 1)[1]
            for pair in query.split("&"):
                if "=" in pair:
                    k, v = pair.split("=", 1)
                    params[k] = v

        device_id = params.get("device_id", "")
        token = params.get("token", "")

        if not device_id:
            await ws.close(4000, "missing device_id")
            return
        if not verify_device_token(device_id, token):
            log(f"DEVICE AUTH FAILED: {device_id}")
            await ws.close(4001, "unauthorized")
            return

        log(f"DEVICE AUTH OK: {device_id}")
        await handle_device(ws, device_id)

    elif path.startswith("/ws/viewer"):
        log("VIEWER CONNECTED")
        await handle_viewer(ws)

    else:
        await ws.close(4004, "unknown path")


# ── Main ───────────────────────────────────────────
async def main():
    parser = argparse.ArgumentParser(description="DashBox Camera Stream Server")
    parser.add_argument("--port", type=int, default=8444)
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--cert", default=None)
    parser.add_argument("--key", default=None)
    args = parser.parse_args()

    ssl_context = None
    if args.cert and args.key:
        import ssl
        ssl_context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        ssl_context.load_cert_chain(args.cert, args.key)
        ssl_context.check_hostname = False
        ssl_context.verify_mode = ssl.CERT_NONE

    log(f"Starting on {args.host}:{args.port} (SSL={ssl_context is not None})")
    async with websockets.serve(router, args.host, args.port, ssl=ssl_context):
        log("Ready")
        await asyncio.Future()


if __name__ == "__main__":
    asyncio.run(main())
