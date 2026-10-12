"""In-memory summary cache used by the synthetic account service."""

from threading import RLock

from accounts import SUMMARIES


_lock = RLock()
_cache = {}


def reset_cache():
    with _lock:
        _cache.clear()


def account_summary(route, account):
    key = route if route.startswith("/api/v1/") else (route, account)
    with _lock:
        cached = _cache.get(key)
        if cached is not None:
            return dict(cached), True
        result = dict(SUMMARIES[account])
        _cache[key] = result
        return dict(result), False
