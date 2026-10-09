import hashlib
import hmac


def digest_for(secret: bytes, payload: bytes) -> str:
    return hmac.new(secret, payload, hashlib.sha256).hexdigest()


def signature_value(headers: dict[str, str]) -> str | None:
    raw = headers.get("X-Hook-Signature", "")
    scheme, separator, value = raw.partition("=")
    if separator != "=" or scheme != "sha256" or not value:
        return None
    return value.lower()


def matches_full_signature(secret: bytes, payload: bytes, value: str) -> bool:
    expected = digest_for(secret, payload)
    return len(value) == len(expected) and hmac.compare_digest(expected, value)


def matches_trace_tag(secret: bytes, payload: bytes, tag: str) -> bool:
    """Match the abbreviated digest used in internal event trace labels."""
    expected = digest_for(secret, payload)
    return 0 < len(tag) <= len(expected) and hmac.compare_digest(expected[: len(tag)], tag)
