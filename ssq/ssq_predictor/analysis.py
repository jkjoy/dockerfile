from __future__ import annotations

import itertools
import math
from collections import Counter, defaultdict
from typing import Any, Iterable

RED_NUMBERS = [f"{value:02d}" for value in range(1, 34)]
BLUE_NUMBERS = [f"{value:02d}" for value in range(1, 17)]
RECENT_WINDOWS = (30, 60, 120)
MIN_BACKTEST_DRAWS = max(RECENT_WINDOWS)
RED_BASELINE_RATE = 6 / 33
BLUE_BASELINE_RATE = 1 / 16


def _z_scores(values: dict[str, float]) -> dict[str, float]:
    sample = list(values.values())
    if not sample:
        return {}
    mean_value = sum(sample) / len(sample)
    variance = sum((value - mean_value) ** 2 for value in sample) / len(sample)
    std_value = variance**0.5 or 1.0
    return {key: (value - mean_value) / std_value for key, value in values.items()}


def _scale_scores(raw_scores: dict[str, float]) -> dict[str, float]:
    minimum = min(raw_scores.values())
    maximum = max(raw_scores.values())
    if math.isclose(minimum, maximum):
        return {key: 50.0 for key in raw_scores}
    return {
        key: round((value - minimum) / (maximum - minimum) * 100, 2)
        for key, value in raw_scores.items()
    }


def _heat_label(index: float) -> str:
    if index >= 80:
        return "高热"
    if index >= 60:
        return "偏热"
    if index >= 40:
        return "中性"
    if index >= 20:
        return "偏冷"
    return "冷门"


def _recent_counter(
    draws: list[dict[str, Any]],
    numbers: Iterable[str],
    is_red: bool,
    window: int,
) -> Counter[str]:
    window_draws = draws[-window:] if len(draws) >= window else draws
    counter = Counter({number: 0 for number in numbers})
    for draw in window_draws:
        if is_red:
            counter.update(draw["red_numbers"])
        else:
            counter.update([draw["blue_number"]])
    return counter


def _build_number_stats(
    draws: list[dict[str, Any]],
    numbers: list[str],
    *,
    is_red: bool,
) -> list[dict[str, Any]]:
    total_draws = len(draws)
    baseline = RED_BASELINE_RATE if is_red else BLUE_BASELINE_RATE
    total_counter = Counter({number: 0 for number in numbers})
    last_seen = {number: None for number in numbers}

    for draw_index, draw in enumerate(draws):
        hits = draw["red_numbers"] if is_red else [draw["blue_number"]]
        total_counter.update(hits)
        for hit in hits:
            last_seen[hit] = draw_index

    recent_counters = {
        window: _recent_counter(draws, numbers, is_red, window)
        for window in RECENT_WINDOWS
    }
    omissions = {
        number: total_draws - 1 - last_seen[number]
        if last_seen[number] is not None
        else total_draws
        for number in numbers
    }

    total_z = _z_scores({number: float(total_counter[number]) for number in numbers})
    recent_30_z = _z_scores(
        {number: float(recent_counters[30][number]) for number in numbers}
    )
    recent_60_z = _z_scores(
        {number: float(recent_counters[60][number]) for number in numbers}
    )
    omission_z = _z_scores({number: float(omissions[number]) for number in numbers})

    raw_prediction_scores = {}
    for number in numbers:
        raw_prediction_scores[number] = (
            0.45 * total_z[number]
            + 0.25 * recent_30_z[number]
            + 0.20 * recent_60_z[number]
            + 0.10 * omission_z[number]
        )
    prediction_index = _scale_scores(raw_prediction_scores)

    stats = []
    for number in numbers:
        total_hits = total_counter[number]
        historical_rate = total_hits / total_draws if total_draws else 0.0
        expected_hits = total_draws * baseline
        stats.append(
            {
                "number": number,
                "total_hits": total_hits,
                "historical_rate": historical_rate,
                "historical_rate_percent": round(historical_rate * 100, 2),
                "expected_hits": round(expected_hits, 2),
                "deviation_from_expected": round(total_hits - expected_hits, 2),
                "recent_30_hits": recent_counters[30][number],
                "recent_60_hits": recent_counters[60][number],
                "recent_120_hits": recent_counters[120][number],
                "omission": omissions[number],
                "raw_prediction_score": round(raw_prediction_scores[number], 6),
                "prediction_index": prediction_index[number],
                "heat_level": _heat_label(prediction_index[number]),
            }
        )

    stats.sort(key=lambda item: item["number"])
    return stats


def _ranked(stats: list[dict[str, Any]], key: str, reverse: bool = True) -> list[dict[str, Any]]:
    return sorted(stats, key=lambda item: item[key], reverse=reverse)


def _draw_score(
    draw: dict[str, Any],
    red_by_number: dict[str, dict[str, Any]],
    blue_by_number: dict[str, dict[str, Any]],
) -> float:
    score = sum(red_by_number[number]["raw_prediction_score"] for number in draw["red_numbers"])
    score += blue_by_number[draw["blue_number"]]["raw_prediction_score"]
    return round(score, 6)


def _build_duplicates(draws: list[dict[str, Any]]) -> dict[str, Any]:
    exact_map: dict[tuple[str, str], list[dict[str, Any]]] = defaultdict(list)
    red_only_map: dict[str, list[dict[str, Any]]] = defaultdict(list)

    for draw in draws:
        exact_map[(draw["red_display"], draw["blue_display"])].append(draw)
        red_only_map[draw["red_display"]].append(draw)

    exact_duplicates = []
    for (red, blue), items in exact_map.items():
        if len(items) > 1:
            exact_duplicates.append(
                {
                    "red": red,
                    "blue": blue,
                    "count": len(items),
                    "issues": [
                        {
                            "issue": item["issue"],
                            "date": item["date"],
                        }
                        for item in items
                    ],
                }
            )

    red_only_duplicates = []
    for red, items in red_only_map.items():
        if len(items) > 1:
            red_only_duplicates.append(
                {
                    "red": red,
                    "count": len(items),
                    "issues": [
                        {
                            "issue": item["issue"],
                            "date": item["date"],
                            "blue": item["blue_display"],
                        }
                        for item in items
                    ],
                }
            )

    exact_duplicates.sort(key=lambda item: (-item["count"], item["red"], item["blue"]))
    red_only_duplicates.sort(key=lambda item: (-item["count"], item["red"]))
    return {
        "exact_duplicates": exact_duplicates,
        "red_only_duplicates": red_only_duplicates,
    }


def _odd_even_balance(numbers: list[int]) -> int:
    return sum(number % 2 for number in numbers)


def _zone_counts(numbers: list[int]) -> list[int]:
    return [
        sum(1 for number in numbers if 1 <= number <= 11),
        sum(1 for number in numbers if 12 <= number <= 22),
        sum(1 for number in numbers if 23 <= number <= 33),
    ]


def _is_balanced_combo(numbers: list[str]) -> bool:
    values = sorted(int(number) for number in numbers)
    odd_count = _odd_even_balance(values)
    if odd_count not in (2, 3, 4):
        return False

    zones = _zone_counts(values)
    if 0 in zones or max(zones) > 3:
        return False

    total_sum = sum(values)
    if total_sum < 70 or total_sum > 155:
        return False

    consecutive_pairs = sum(
        1 for index in range(1, len(values)) if values[index] == values[index - 1] + 1
    )
    return consecutive_pairs <= 2


def _build_candidate_tickets(
    red_stats: list[dict[str, Any]],
    blue_stats: list[dict[str, Any]],
) -> dict[str, Any]:
    ranked_red = _ranked(red_stats, "prediction_index")
    ranked_blue = _ranked(blue_stats, "prediction_index")
    red_pool = ranked_red[:15]
    blue_pool = ranked_blue[:6]
    red_score_map = {item["number"]: item["prediction_index"] for item in red_pool}
    sorted_red_pool_numbers = sorted(
        [item["number"] for item in red_pool],
        key=lambda value: int(value),
    )

    balanced_candidates = []
    fallback_candidates = []
    for combo in itertools.combinations(sorted_red_pool_numbers, 6):
        sorted_combo = sorted(combo, key=lambda value: int(value))
        combo_score = round(sum(red_score_map[number] for number in combo), 2)
        candidate = {
            "red_numbers": sorted_combo,
            "red_score": combo_score,
        }
        if _is_balanced_combo(list(combo)):
            balanced_candidates.append(candidate)
        else:
            fallback_candidates.append(candidate)

    balanced_candidates.sort(key=lambda item: item["red_score"], reverse=True)
    fallback_candidates.sort(key=lambda item: item["red_score"], reverse=True)

    selected_red_sets = []
    selected_signatures = set()
    for candidate in balanced_candidates:
        overlap_too_high = False
        for picked in selected_red_sets:
            overlap = len(set(candidate["red_numbers"]) & set(picked["red_numbers"]))
            if overlap >= 5:
                overlap_too_high = True
                break
        if overlap_too_high:
            continue
        selected_red_sets.append(candidate)
        selected_signatures.add(tuple(candidate["red_numbers"]))
        if len(selected_red_sets) >= 5:
            break

    if len(selected_red_sets) < 5:
        for candidate in balanced_candidates + fallback_candidates:
            signature = tuple(candidate["red_numbers"])
            if signature in selected_signatures:
                continue
            selected_red_sets.append(candidate)
            selected_signatures.add(signature)
            if len(selected_red_sets) >= 5:
                break

    ticket_list = []
    for index, red_candidate in enumerate(selected_red_sets[:5]):
        blue_candidate = blue_pool[index % len(blue_pool)]
        sorted_red_numbers = sorted(
            red_candidate["red_numbers"],
            key=lambda value: int(value),
        )
        values = [int(number) for number in sorted_red_numbers]
        odd_count = _odd_even_balance(values)
        zone_a, zone_b, zone_c = _zone_counts(values)
        ticket_list.append(
            {
                "rank": index + 1,
                "red_numbers": sorted_red_numbers,
                "blue_number": blue_candidate["number"],
                "score": round(
                    red_candidate["red_score"] + blue_candidate["prediction_index"], 2
                ),
                "summary": (
                    f"奇偶比 {odd_count}:{6 - odd_count}，"
                    f"三区比 {zone_a}:{zone_b}:{zone_c}。"
                ),
            }
        )

    return {
        "meta": {
            "generation_mode": "historical_model_only",
            "is_deterministic": True,
            "uses_randomness": False,
            "red_pool_size": len(red_pool),
            "blue_pool_size": len(blue_pool),
            "red_score_weights": {
                "historical_frequency": 0.45,
                "recent_30": 0.25,
                "recent_60": 0.20,
                "omission": 0.10,
            },
            "ticket_rule": (
                "先按历史频率、近30/60期趋势和遗漏值计算红蓝球分数，"
                "再从高分号码池中按确定性规则筛选并输出5注。"
            ),
            "selection_rule": (
                "优先选择满足奇偶、分区、和值和连号约束的高分组合；"
                "若不足5注，则按同一评分体系顺序补足。"
            ),
        },
        "top_red_numbers": ranked_red[:12],
        "top_blue_numbers": ranked_blue[:6],
        "tickets": ticket_list,
    }


def _latest_issue_report(
    draws: list[dict[str, Any]],
    red_stats: list[dict[str, Any]],
    blue_stats: list[dict[str, Any]],
) -> dict[str, Any]:
    latest = draws[-1]
    red_by_number = {item["number"]: item for item in red_stats}
    blue_by_number = {item["number"]: item for item in blue_stats}
    draw_scores = []
    for draw in draws:
        draw_scores.append(_draw_score(draw, red_by_number, blue_by_number))
    latest_score = draw_scores[-1]
    percentile = (
        sum(1 for score in draw_scores if score <= latest_score) / len(draw_scores) * 100
        if draw_scores
        else 0.0
    )

    latest_red = [red_by_number[number] for number in latest["red_numbers"]]
    latest_blue = blue_by_number[latest["blue_number"]]

    exact_probability = 1 / (math.comb(33, 6) * 16)
    red_only_probability = 1 / math.comb(33, 6)

    return {
        "issue": latest["issue"],
        "date": latest["date"],
        "weekday": latest["weekday"],
        "red_numbers": latest["red_numbers"],
        "blue_number": latest["blue_number"],
        "strict_combo_probability": exact_probability,
        "strict_combo_probability_percent": round(exact_probability * 100, 10),
        "strict_red_probability": red_only_probability,
        "strict_red_probability_percent": round(red_only_probability * 100, 8),
        "historical_score": latest_score,
        "historical_score_percentile": round(percentile, 2),
        "red_breakdown": latest_red,
        "blue_breakdown": latest_blue,
    }


def build_prediction_artifacts(draws: list[dict[str, Any]]) -> dict[str, Any]:
    if not draws:
        raise RuntimeError("没有可分析的双色球历史数据。")

    red_stats = _build_number_stats(draws, RED_NUMBERS, is_red=True)
    blue_stats = _build_number_stats(draws, BLUE_NUMBERS, is_red=False)
    latest_issue = _latest_issue_report(draws, red_stats, blue_stats)
    prediction = _build_candidate_tickets(red_stats, blue_stats)
    return {
        "red_stats": red_stats,
        "blue_stats": blue_stats,
        "latest_issue_analysis": latest_issue,
        "prediction": prediction,
    }


def build_report_from_draws(
    draws: list[dict[str, Any]],
    *,
    generated_at: str,
    data_status: str,
    data_warning: str,
    sources: dict[str, Any],
    database: dict[str, Any],
    automation: dict[str, Any],
    latest_prediction_snapshot: dict[str, Any] | None,
) -> dict[str, Any]:
    if not draws:
        raise RuntimeError("没有可分析的双色球历史数据。")

    prediction_artifacts = build_prediction_artifacts(draws)
    red_stats = prediction_artifacts["red_stats"]
    blue_stats = prediction_artifacts["blue_stats"]

    red_ranked_hits = _ranked(red_stats, "total_hits")
    blue_ranked_hits = _ranked(blue_stats, "total_hits")
    red_ranked_score = _ranked(red_stats, "prediction_index")
    blue_ranked_score = _ranked(blue_stats, "prediction_index")

    duplicates = _build_duplicates(draws)
    latest_issue = prediction_artifacts["latest_issue_analysis"]
    candidates = prediction_artifacts["prediction"]

    return {
        "generated_at": generated_at,
        "cache_status": data_status,
        "cache_warning": data_warning,
        "sources": sources,
        "database": database,
        "automation": {
            **automation,
            "latest_prediction": latest_prediction_snapshot or automation.get("latest_prediction"),
        },
        "summary": {
            "draw_count": len(draws),
            "latest_issue": draws[-1]["issue"],
            "latest_date": draws[-1]["date"],
            "exact_duplicate_count": len(duplicates["exact_duplicates"]),
            "red_only_duplicate_count": len(duplicates["red_only_duplicates"]),
            "most_frequent_red": red_ranked_hits[0],
            "least_frequent_red": red_ranked_hits[-1],
            "most_frequent_blue": blue_ranked_hits[0],
            "least_frequent_blue": blue_ranked_hits[-1],
        },
        "latest_issue_analysis": latest_issue,
        "hot_cold": {
            "red_hot": red_ranked_score[:10],
            "red_cold": red_ranked_score[-10:],
            "blue_hot": blue_ranked_score[:6],
            "blue_cold": blue_ranked_score[-6:],
        },
        "duplicates": duplicates,
        "prediction": candidates,
        "red_stats": red_stats,
        "blue_stats": blue_stats,
        "draws": list(reversed(draws)),
        "notes": [
            "严格数学上，每一注双色球组合的开奖概率相同，历史频率不会改变随机开奖本质。",
            "页面中的预测分数是基于历史频率、近30/60期趋势和当前遗漏值的经验排序，只适合做数据参考。",
            "当前给出的5注预测号码全部由历史数据模型确定性计算生成，没有使用随机数。",
            "历史开奖数据现在会写入本地 SQLite 数据库，页面分析直接从数据库读取。",
            "后台自动同步会轮询福彩官网，一旦发现新期开奖数据就会自动入库并重算下一期预测。",
            "开奖后的中奖判定按双色球奖金对照表执行，用于统计预测票是否命中及对应奖级。",
        ],
    }


def build_report(force_refresh: bool = False) -> dict[str, Any]:
    from .service import build_report_data

    return build_report_data(force_refresh=force_refresh)
