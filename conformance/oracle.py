"""Conformance oracle: a thin newline-delimited JSON-RPC wrapper around the
reference `jton` implementation. The Go conformance test drives this process and
compares its output byte-for-byte against the Go port.

This file contains no business logic — it only invokes the reference library so
the Go side can verify cross-language equivalence.

Request  (one JSON object per line):
    {"op": "dumps",      "b64": "<base64 of standard-JSON input>", "opts": {...}}
    {"op": "loads_dump", "b64": "<base64 of JTON input>",         "opts": {...}}
Response (one JSON object per line):
    {"ok": true,  "out": "<jton.dumps result>"}
    {"ok": false, "err": "<ExceptionType: message>"}
"""

import base64
import json
import sys

import jton


def handle(req):
    op = req["op"]
    data = base64.b64decode(req["b64"])
    opts = req.get("opts", {})
    if op == "dumps":
        value = json.loads(data)
        return {"ok": True, "out": jton.dumps(value, **opts)}
    if op == "loads_dump":
        value = jton.loads(data)
        return {"ok": True, "out": jton.dumps(value, **opts)}
    return {"ok": False, "err": "unknown op: " + str(op)}


def main():
    out = sys.stdout
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            resp = handle(json.loads(line))
        except BaseException as e:  # noqa: BLE001 - also trap Rust PanicException
            resp = {"ok": False, "err": type(e).__name__ + ": " + str(e)}
        out.write(json.dumps(resp))
        out.write("\n")
        out.flush()


if __name__ == "__main__":
    main()
