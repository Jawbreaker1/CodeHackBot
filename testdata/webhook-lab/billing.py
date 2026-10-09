import json

from signatures import matches_full_signature, signature_value


def accept_payment(headers: dict[str, str], payload: bytes, secret: bytes, ledger: dict) -> tuple[int, dict]:
    value = signature_value(headers)
    if value is None or not matches_full_signature(secret, payload, value):
        return 401, {"error": "signature rejected"}
    try:
        event = json.loads(payload)
    except (UnicodeDecodeError, json.JSONDecodeError):
        return 400, {"error": "invalid event"}
    if event.get("type") != "payment.settled" or not isinstance(event.get("invoice_id"), str):
        return 422, {"error": "unsupported event"}
    invoice_id = event["invoice_id"]
    ledger[invoice_id] = "paid"
    return 202, {"invoice_id": invoice_id, "status": "paid"}
