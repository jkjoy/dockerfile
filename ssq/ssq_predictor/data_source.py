from __future__ import annotations

import json
import re
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timedelta
from pathlib import Path
from typing import Any

import requests

OFFICIAL_PAGE_URL = "https://www.cwl.gov.cn/ygkj/wqkjgg/ssq/"
OFFICIAL_API_URL = (
    "https://www.cwl.gov.cn/cwl_admin/front/cwlkj/search/kjxx/findDrawNotice"
)
FALLBACK_SOURCE_PAGE_URL = "https://api.huiniao.top/"
FALLBACK_API_URL = "https://api.huiniao.top/interface/home/lotteryHistory"
DEFAULT_CACHE_PATH = Path(__file__).resolve().parent.parent / "data" / "ssq_history.json"
DEFAULT_PAGE_SIZE = 30
FALLBACK_PAGE_SIZE = 500
DEFAULT_CACHE_MAX_AGE_HOURS = 12
REQUEST_TIMEOUT = 20
MAX_RETRIES = 5
USER_AGENT = (
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
    "(KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36"
)
API_ACCEPT = "application/json, text/javascript, */*; q=0.01"
HTML_ACCEPT = (
    "text/html,application/xhtml+xml,application/xml;q=0.9,"
    "image/avif,image/webp,*/*;q=0.8"
)


class FetchError(RuntimeError):
    pass


WEEKDAY_LABELS = ("一", "二", "三", "四", "五", "六", "日")


def _now_iso() -> str:
    return datetime.now().isoformat(timespec="seconds")


def _ensure_parent(path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)


def _parse_draw(raw: dict[str, Any]) -> dict[str, Any]:
    match = re.match(r"(\d{4}-\d{2}-\d{2})\((.)\)", raw.get("date", ""))
    draw_date = match.group(1) if match else raw.get("date", "")[:10]
    weekday = match.group(2) if match else ""
    red_numbers = [part.strip() for part in raw.get("red", "").split(",") if part.strip()]
    blue_number = raw.get("blue", "").strip()
    return {
        "issue": raw.get("code", "").strip(),
        "date": draw_date,
        "weekday": weekday,
        "red_numbers": red_numbers,
        "blue_number": blue_number,
        "red_display": ",".join(red_numbers),
        "blue_display": blue_number,
        "sales": raw.get("sales", ""),
        "poolmoney": raw.get("poolmoney", ""),
        "content": raw.get("content", ""),
        "details_link": raw.get("detailsLink", ""),
    }


def _weekday_label(draw_date: str) -> str:
    try:
        return WEEKDAY_LABELS[datetime.strptime(draw_date, "%Y-%m-%d").weekday()]
    except ValueError:
        return ""


def _normalize_ball(number: str) -> str:
    text = str(number).strip()
    if not text:
        return ""
    if text.isdigit():
        return f"{int(text):02d}"
    return text


def _parse_fallback_draw(raw: dict[str, Any]) -> dict[str, Any]:
    draw_date = raw.get("day", "").strip()
    red_numbers = [
        _normalize_ball(raw.get("one", "")),
        _normalize_ball(raw.get("two", "")),
        _normalize_ball(raw.get("three", "")),
        _normalize_ball(raw.get("four", "")),
        _normalize_ball(raw.get("five", "")),
        _normalize_ball(raw.get("six", "")),
    ]
    red_numbers = [number for number in red_numbers if number]
    blue_number = _normalize_ball(raw.get("seven", ""))
    return {
        "issue": raw.get("code", "").strip(),
        "date": draw_date,
        "weekday": _weekday_label(draw_date),
        "red_numbers": red_numbers,
        "blue_number": blue_number,
        "red_display": ",".join(red_numbers),
        "blue_display": blue_number,
        "sales": raw.get("sales", ""),
        "poolmoney": raw.get("poolmoney", ""),
        "content": raw.get("content", ""),
        "details_link": raw.get("detailsLink", ""),
    }


def _headers(
    *,
    referer: str | None = None,
    expect_json: bool = True,
) -> dict[str, str]:
    headers = {
        "User-Agent": USER_AGENT,
        "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
        "Cache-Control": "no-cache",
        "Pragma": "no-cache",
    }
    if expect_json:
        headers["Accept"] = API_ACCEPT
        headers["X-Requested-With"] = "XMLHttpRequest"
    else:
        headers["Accept"] = HTML_ACCEPT
        headers["Upgrade-Insecure-Requests"] = "1"
    if referer:
        headers["Referer"] = referer
    return headers


def _create_session() -> requests.Session:
    return requests.Session()


def _warm_up_official_page(session: requests.Session) -> None:
    try:
        session.get(
            OFFICIAL_PAGE_URL,
            headers=_headers(expect_json=False),
            timeout=REQUEST_TIMEOUT,
        )
    except requests.RequestException:
        pass


def _fetch_page(
    session: requests.Session,
    *,
    page_no: int,
    page_size: int = DEFAULT_PAGE_SIZE,
) -> dict[str, Any]:
    params = {
        "name": "ssq",
        "pageNo": page_no,
        "pageSize": page_size,
        "systemType": "PC",
    }
    response = None
    last_error = None
    for attempt in range(MAX_RETRIES):
        for headers in (
            _headers(referer=OFFICIAL_PAGE_URL),
            _headers(),
        ):
            try:
                response = session.get(
                    OFFICIAL_API_URL,
                    params=params,
                    headers=headers,
                    timeout=REQUEST_TIMEOUT,
                )
            except requests.RequestException as exc:
                last_error = exc
                continue
            if response.status_code == 200:
                break
            last_error = FetchError(
                f"官方接口返回状态码 {response.status_code}，第 {page_no} 页"
            )
        else:
            response = None

        if response is not None and response.status_code == 200:
            break

        _warm_up_official_page(session)
        time.sleep(0.8 * (attempt + 1))
    else:
        raise FetchError(f"抓取双色球数据失败: {last_error}") from last_error

    assert response is not None
    try:
        payload = response.json()
    except ValueError as exc:
        raise FetchError(f"官方接口返回了非 JSON 数据，第 {page_no} 页") from exc
    if payload.get("state") != 0:
        raise FetchError(f"官方接口返回异常: {payload.get('message', '未知错误')}")
    return payload


def _fetch_fallback_page(
    session: requests.Session,
    *,
    page_no: int,
    page_size: int = FALLBACK_PAGE_SIZE,
) -> dict[str, Any]:
    last_error = None
    for attempt in range(MAX_RETRIES):
        response = session.get(
            FALLBACK_API_URL,
            params={
                "type": "ssq",
                "page": page_no,
                "limit": page_size,
            },
            headers={
                "User-Agent": USER_AGENT,
                "Accept": "application/json, text/plain, */*",
            },
            timeout=REQUEST_TIMEOUT,
        )
        response.raise_for_status()
        try:
            payload = response.json()
        except ValueError as exc:
            raise FetchError(f"备用接口返回了非 JSON 数据，第 {page_no} 页") from exc

        if payload.get("code") == 1:
            data = payload.get("data", {}).get("data", {})
            if not isinstance(data, dict):
                raise FetchError("备用接口返回结构异常。")
            return payload

        info = str(payload.get("info", "未知错误"))
        last_error = FetchError(f"备用接口返回异常: {info}")
        if "请求过于频繁" not in info:
            break
        time.sleep(1.2 * (attempt + 1))

    assert last_error is not None
    raise last_error


def _fetch_fallback_page_with_new_session(
    *,
    page_no: int,
    page_size: int,
) -> dict[str, Any]:
    return _fetch_fallback_page(
        _create_session(),
        page_no=page_no,
        page_size=page_size,
    )


def _build_history_payload(
    *,
    fetched_at: str,
    source_name: str,
    source_page_url: str,
    source_api_url: str,
    draw_count: int,
    official_total: int | None,
    latest_issue: str | None,
    stop_issue: str | None,
    stop_issue_found: bool,
    draws: list[dict[str, Any]],
    source_warning: str = "",
) -> dict[str, Any]:
    return {
        "fetched_at": fetched_at,
        "source_name": source_name,
        "source_page_url": source_page_url,
        "source_api_url": source_api_url,
        "draw_count": draw_count,
        "official_total": official_total,
        "latest_issue": latest_issue,
        "stop_issue": stop_issue,
        "stop_issue_found": stop_issue_found,
        "draws": draws,
        "source_warning": source_warning,
    }


def _fetch_history_from_official(stop_issue: str | None = None) -> dict[str, Any]:
    session = _create_session()
    draws: list[dict[str, Any]] = []
    total_pages: int | None = None
    total_draws: int | None = None
    page_no = 1
    stop_issue_found = False
    latest_issue: str | None = None

    while total_pages is None or page_no <= total_pages:
        payload = _fetch_page(session, page_no=page_no)
        total_pages = int(payload.get("pageNum", 0))
        total_draws = int(payload.get("total", 0))
        if latest_issue is None and payload.get("result"):
            latest_issue = payload["result"][0].get("code", "").strip()

        for item in payload.get("result", []):
            parsed_draw = _parse_draw(item)
            if stop_issue and parsed_draw["issue"] == stop_issue:
                stop_issue_found = True
                break
            draws.append(parsed_draw)
        if stop_issue_found:
            break
        page_no += 1
        time.sleep(0.15)

    draws.sort(key=lambda item: item["issue"])
    return _build_history_payload(
        fetched_at=_now_iso(),
        source_name="official",
        source_page_url=OFFICIAL_PAGE_URL,
        source_api_url=OFFICIAL_API_URL,
        draw_count=len(draws),
        official_total=total_draws,
        latest_issue=latest_issue,
        stop_issue=stop_issue,
        stop_issue_found=stop_issue_found,
        draws=draws,
    )


def _fetch_history_from_fallback(stop_issue: str | None = None) -> dict[str, Any]:
    session = _create_session()
    draws: list[dict[str, Any]] = []
    page_payloads: dict[int, dict[str, Any]] = {}
    total_draws: int | None = None
    stop_issue_found = False
    latest_issue: str | None = None

    first_page = _fetch_fallback_page(
        session,
        page_no=1,
        page_size=FALLBACK_PAGE_SIZE,
    )
    page_payloads[1] = first_page
    first_page_data = first_page.get("data", {}).get("data", {})
    total_pages = int(first_page_data.get("totalPage", 0))
    total_draws = int(first_page_data.get("totalCount", 0))

    if stop_issue is None and total_pages > 1:
        max_workers = min(3, total_pages - 1)
        with ThreadPoolExecutor(max_workers=max_workers) as executor:
            future_map = {
                executor.submit(
                    _fetch_fallback_page_with_new_session,
                    page_no=page_no,
                    page_size=FALLBACK_PAGE_SIZE,
                ): page_no
                for page_no in range(2, total_pages + 1)
            }
            for future in as_completed(future_map):
                page_payloads[future_map[future]] = future.result()

    for page_no in range(1, total_pages + 1):
        if page_no not in page_payloads:
            page_payloads[page_no] = _fetch_fallback_page(
                session,
                page_no=page_no,
                page_size=FALLBACK_PAGE_SIZE,
            )
        payload = page_payloads[page_no]
        page_data = payload.get("data", {}).get("data", {})
        items = page_data.get("list", [])
        if latest_issue is None and items:
            latest_issue = items[0].get("code", "").strip()
        for item in items:
            parsed_draw = _parse_fallback_draw(item)
            if stop_issue and parsed_draw["issue"] == stop_issue:
                stop_issue_found = True
                break
            draws.append(parsed_draw)
        if stop_issue_found:
            break

    draws.sort(key=lambda item: item["issue"])
    return _build_history_payload(
        fetched_at=_now_iso(),
        source_name="fallback_huiniao",
        source_page_url=FALLBACK_SOURCE_PAGE_URL,
        source_api_url=FALLBACK_API_URL,
        draw_count=len(draws),
        official_total=total_draws,
        latest_issue=latest_issue,
        stop_issue=stop_issue,
        stop_issue_found=stop_issue_found,
        draws=draws,
    )


def load_cache(cache_path: Path = DEFAULT_CACHE_PATH) -> dict[str, Any] | None:
    if not cache_path.exists():
        return None
    return json.loads(cache_path.read_text(encoding="utf-8"))


def save_cache(payload: dict[str, Any], cache_path: Path = DEFAULT_CACHE_PATH) -> None:
    _ensure_parent(cache_path)
    cache_path.write_text(
        json.dumps(payload, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )


def _cache_is_fresh(payload: dict[str, Any], max_age_hours: int) -> bool:
    fetched_at = payload.get("fetched_at")
    if not fetched_at:
        return False
    try:
        fetched_dt = datetime.fromisoformat(fetched_at)
    except ValueError:
        return False
    return datetime.now() - fetched_dt <= timedelta(hours=max_age_hours)


def fetch_history_from_official(stop_issue: str | None = None) -> dict[str, Any]:
    errors = []
    try:
        return _fetch_history_from_official(stop_issue=stop_issue)
    except Exception as exc:
        errors.append(f"官方接口失败: {exc}")

    try:
        payload = _fetch_history_from_fallback(stop_issue=stop_issue)
        payload["source_warning"] = (
            "官方接口当前不可用，本次已自动切换到备用数据源。"
        )
        payload["source_errors"] = errors
        return payload
    except Exception as exc:
        errors.append(f"备用接口失败: {exc}")
        raise FetchError("；".join(errors)) from exc


def get_history(
    force_refresh: bool = False,
    cache_path: Path = DEFAULT_CACHE_PATH,
    cache_max_age_hours: int = DEFAULT_CACHE_MAX_AGE_HOURS,
) -> dict[str, Any]:
    cached = load_cache(cache_path)
    if cached and not force_refresh and _cache_is_fresh(cached, cache_max_age_hours):
        cached["cache_status"] = "fresh"
        return cached

    try:
        payload = fetch_history_from_official()
        payload["cache_status"] = "refreshed"
        save_cache(payload, cache_path)
        return payload
    except Exception as exc:
        if cached:
            cached["cache_status"] = "stale_fallback"
            cached["cache_warning"] = (
                "官方接口请求失败，当前展示的是本地缓存数据。"
                f"失败原因: {exc}"
            )
            return cached
        raise
