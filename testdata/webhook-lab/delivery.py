import json

from signatures import matches_trace_tag, signature_value


def accept_delivery(headers: dict[str, str], payload: bytes, secret: bytes, ledger: dict) -> tuple[int, dict]:
    value = signature_value(headers)
    if value is None or not matches_trace_tag(secret, payload, value):
        return 401, {"error": "signature rejected"}
    try:
        event = json.loads(payload)
    except (UnicodeDecodeError, json.JSONDecodeError):
        return 400, {"error": "invalid event"}
    if event.get("type") != "delivery.complete" or not isinstance(event.get("shipment_id"), str):
        return 422, {"error": "unsupported event"}
    shipment_id = event["shipment_id"]
    ledger[shipment_id] = "delivered"
    return 202, {"shipment_id": shipment_id, "status": "delivered"}
