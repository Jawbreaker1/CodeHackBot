from billing import accept_payment
from delivery import accept_delivery


ROUTES = {
    "/hooks/delivery": accept_delivery,
    "/hooks/billing": accept_payment,
}


def dispatch(
    method: str,
    path: str,
    headers: dict[str, str],
    payload: bytes,
    secret: bytes,
    ledger: dict,
) -> tuple[int, dict]:
    if method != "POST":
        return 405, {"error": "method not allowed"}
    handler = ROUTES.get(path)
    if handler is None:
        return 404, {"error": "route not found"}
    return handler(headers, payload, secret, ledger)
