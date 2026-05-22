from __future__ import annotations

import json
import sqlite3
from contextlib import contextmanager
from datetime import datetime
from pathlib import Path
from typing import Any, Iterator

DEFAULT_DB_PATH = Path(__file__).resolve().parent.parent / "data" / "ssq.db"


def _now_iso() -> str:
    return datetime.now().isoformat(timespec="seconds")


def _ensure_parent(path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)


@contextmanager
def get_connection(db_path: Path = DEFAULT_DB_PATH) -> Iterator[sqlite3.Connection]:
    _ensure_parent(db_path)
    connection = sqlite3.connect(db_path)
    connection.row_factory = sqlite3.Row
    try:
        yield connection
    finally:
        connection.close()


def initialize_database(db_path: Path = DEFAULT_DB_PATH) -> None:
    with get_connection(db_path) as connection:
        connection.execute("PRAGMA journal_mode=WAL")
        connection.execute(
            """
            CREATE TABLE IF NOT EXISTS draws (
                issue TEXT PRIMARY KEY,
                draw_date TEXT NOT NULL,
                weekday TEXT NOT NULL,
                red_numbers TEXT NOT NULL,
                blue_number TEXT NOT NULL,
                red_display TEXT NOT NULL,
                blue_display TEXT NOT NULL,
                sales TEXT NOT NULL,
                poolmoney TEXT NOT NULL,
                content TEXT NOT NULL,
                details_link TEXT NOT NULL,
                created_at TEXT NOT NULL,
                updated_at TEXT NOT NULL
            )
            """
        )
        connection.execute(
            """
            CREATE TABLE IF NOT EXISTS sync_runs (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                started_at TEXT NOT NULL,
                finished_at TEXT NOT NULL,
                trigger_type TEXT NOT NULL,
                sync_mode TEXT NOT NULL,
                status TEXT NOT NULL,
                fetched_count INTEGER NOT NULL,
                inserted_count INTEGER NOT NULL,
                updated_count INTEGER NOT NULL,
                latest_issue TEXT,
                message TEXT
            )
            """
        )
        connection.execute(
            """
            CREATE TABLE IF NOT EXISTS prediction_snapshots (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                base_issue TEXT NOT NULL UNIQUE,
                base_date TEXT NOT NULL,
                target_issue TEXT,
                target_label TEXT NOT NULL,
                generated_at TEXT NOT NULL,
                model_version TEXT NOT NULL,
                draw_count INTEGER NOT NULL,
                top_red_numbers_json TEXT NOT NULL,
                top_blue_numbers_json TEXT NOT NULL,
                tickets_json TEXT NOT NULL,
                summary_json TEXT NOT NULL
            )
            """
        )
        connection.execute(
            """
            CREATE TABLE IF NOT EXISTS prediction_evaluations (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                snapshot_id INTEGER NOT NULL UNIQUE,
                base_issue TEXT NOT NULL,
                target_issue TEXT NOT NULL,
                target_date TEXT NOT NULL,
                actual_red_numbers TEXT NOT NULL,
                actual_blue_number TEXT NOT NULL,
                evaluated_at TEXT NOT NULL,
                ticket_count INTEGER NOT NULL,
                winning_ticket_count INTEGER NOT NULL,
                winning_ticket_rate REAL NOT NULL,
                highest_prize_level TEXT,
                prize_breakdown_json TEXT NOT NULL,
                ticket_results_json TEXT NOT NULL,
                model_version TEXT NOT NULL,
                FOREIGN KEY(snapshot_id) REFERENCES prediction_snapshots(id)
            )
            """
        )
        connection.execute(
            "CREATE INDEX IF NOT EXISTS idx_draws_draw_date ON draws(draw_date)"
        )
        connection.execute(
            "CREATE INDEX IF NOT EXISTS idx_sync_runs_finished_at ON sync_runs(finished_at DESC)"
        )
        connection.execute(
            """
            CREATE INDEX IF NOT EXISTS idx_prediction_snapshots_generated_at
            ON prediction_snapshots(generated_at DESC)
            """
        )
        connection.execute(
            """
            CREATE INDEX IF NOT EXISTS idx_prediction_evaluations_target_issue
            ON prediction_evaluations(target_issue)
            """
        )
        connection.commit()


def _draw_record(draw: dict[str, Any]) -> tuple[Any, ...]:
    return (
        draw["date"],
        draw["weekday"],
        ",".join(draw["red_numbers"]),
        draw["blue_number"],
        draw["red_display"],
        draw["blue_display"],
        draw.get("sales", ""),
        draw.get("poolmoney", ""),
        draw.get("content", ""),
        draw.get("details_link", ""),
    )


def upsert_draws(
    draws: list[dict[str, Any]],
    db_path: Path = DEFAULT_DB_PATH,
) -> dict[str, int]:
    if not draws:
        return {"inserted_count": 0, "updated_count": 0}

    issues = [draw["issue"] for draw in draws]
    placeholders = ",".join("?" for _ in issues)
    inserted_count = 0
    updated_count = 0
    now = _now_iso()

    with get_connection(db_path) as connection:
        existing = {
            row["issue"]: row
            for row in connection.execute(
                f"""
                SELECT
                    issue,
                    draw_date,
                    weekday,
                    red_numbers,
                    blue_number,
                    red_display,
                    blue_display,
                    sales,
                    poolmoney,
                    content,
                    details_link
                FROM draws
                WHERE issue IN ({placeholders})
                """,
                issues,
            )
        }

        for draw in draws:
            current_record = _draw_record(draw)
            row = existing.get(draw["issue"])
            if row is None:
                connection.execute(
                    """
                    INSERT INTO draws (
                        issue,
                        draw_date,
                        weekday,
                        red_numbers,
                        blue_number,
                        red_display,
                        blue_display,
                        sales,
                        poolmoney,
                        content,
                        details_link,
                        created_at,
                        updated_at
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                    """,
                    (draw["issue"], *current_record, now, now),
                )
                inserted_count += 1
                continue

            previous_record = (
                row["draw_date"],
                row["weekday"],
                row["red_numbers"],
                row["blue_number"],
                row["red_display"],
                row["blue_display"],
                row["sales"],
                row["poolmoney"],
                row["content"],
                row["details_link"],
            )
            if previous_record == current_record:
                continue

            connection.execute(
                """
                UPDATE draws
                SET
                    draw_date = ?,
                    weekday = ?,
                    red_numbers = ?,
                    blue_number = ?,
                    red_display = ?,
                    blue_display = ?,
                    sales = ?,
                    poolmoney = ?,
                    content = ?,
                    details_link = ?,
                    updated_at = ?
                WHERE issue = ?
                """,
                (*current_record, now, draw["issue"]),
            )
            updated_count += 1

        connection.commit()

    return {
        "inserted_count": inserted_count,
        "updated_count": updated_count,
    }


def get_draw_count(db_path: Path = DEFAULT_DB_PATH) -> int:
    with get_connection(db_path) as connection:
        row = connection.execute("SELECT COUNT(*) AS count FROM draws").fetchone()
    return int(row["count"]) if row else 0


def get_latest_draw(db_path: Path = DEFAULT_DB_PATH) -> dict[str, Any] | None:
    with get_connection(db_path) as connection:
        row = connection.execute(
            """
            SELECT issue, draw_date
            FROM draws
            ORDER BY issue DESC
            LIMIT 1
            """
        ).fetchone()
    return dict(row) if row else None


def get_all_draws(db_path: Path = DEFAULT_DB_PATH) -> list[dict[str, Any]]:
    with get_connection(db_path) as connection:
        rows = connection.execute(
            """
            SELECT
                issue,
                draw_date,
                weekday,
                red_numbers,
                blue_number,
                red_display,
                blue_display,
                sales,
                poolmoney,
                content,
                details_link
            FROM draws
            ORDER BY issue ASC
            """
        ).fetchall()

    draws = []
    for row in rows:
        draws.append(
            {
                "issue": row["issue"],
                "date": row["draw_date"],
                "weekday": row["weekday"],
                "red_numbers": row["red_numbers"].split(","),
                "blue_number": row["blue_number"],
                "red_display": row["red_display"],
                "blue_display": row["blue_display"],
                "sales": row["sales"],
                "poolmoney": row["poolmoney"],
                "content": row["content"],
                "details_link": row["details_link"],
            }
        )
    return draws


def record_sync_run(
    *,
    started_at: str,
    finished_at: str,
    trigger_type: str,
    sync_mode: str,
    status: str,
    fetched_count: int,
    inserted_count: int,
    updated_count: int,
    latest_issue: str | None,
    message: str,
    db_path: Path = DEFAULT_DB_PATH,
) -> None:
    with get_connection(db_path) as connection:
        connection.execute(
            """
            INSERT INTO sync_runs (
                started_at,
                finished_at,
                trigger_type,
                sync_mode,
                status,
                fetched_count,
                inserted_count,
                updated_count,
                latest_issue,
                message
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                started_at,
                finished_at,
                trigger_type,
                sync_mode,
                status,
                fetched_count,
                inserted_count,
                updated_count,
                latest_issue,
                message,
            ),
        )
        connection.commit()


def get_latest_sync_run(db_path: Path = DEFAULT_DB_PATH) -> dict[str, Any] | None:
    with get_connection(db_path) as connection:
        row = connection.execute(
            """
            SELECT
                id,
                started_at,
                finished_at,
                trigger_type,
                sync_mode,
                status,
                fetched_count,
                inserted_count,
                updated_count,
                latest_issue,
                message
            FROM sync_runs
            ORDER BY id DESC
            LIMIT 1
            """
        ).fetchone()
    return dict(row) if row else None


def get_prediction_snapshot_count(db_path: Path = DEFAULT_DB_PATH) -> int:
    with get_connection(db_path) as connection:
        row = connection.execute(
            "SELECT COUNT(*) AS count FROM prediction_snapshots"
        ).fetchone()
    return int(row["count"]) if row else 0


def get_prediction_evaluation_count(db_path: Path = DEFAULT_DB_PATH) -> int:
    with get_connection(db_path) as connection:
        row = connection.execute(
            "SELECT COUNT(*) AS count FROM prediction_evaluations"
        ).fetchone()
    return int(row["count"]) if row else 0


def save_prediction_snapshot(
    snapshot: dict[str, Any],
    db_path: Path = DEFAULT_DB_PATH,
) -> None:
    save_prediction_snapshots_bulk([snapshot], db_path=db_path)


def save_prediction_snapshots_bulk(
    snapshots: list[dict[str, Any]],
    db_path: Path = DEFAULT_DB_PATH,
) -> None:
    if not snapshots:
        return
    with get_connection(db_path) as connection:
        connection.executemany(
            """
            INSERT INTO prediction_snapshots (
                base_issue,
                base_date,
                target_issue,
                target_label,
                generated_at,
                model_version,
                draw_count,
                top_red_numbers_json,
                top_blue_numbers_json,
                tickets_json,
                summary_json
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(base_issue) DO UPDATE SET
                base_date = excluded.base_date,
                target_issue = excluded.target_issue,
                target_label = excluded.target_label,
                generated_at = excluded.generated_at,
                model_version = excluded.model_version,
                draw_count = excluded.draw_count,
                top_red_numbers_json = excluded.top_red_numbers_json,
                top_blue_numbers_json = excluded.top_blue_numbers_json,
                tickets_json = excluded.tickets_json,
                summary_json = excluded.summary_json
            """,
            [
                (
                    snapshot["base_issue"],
                    snapshot["base_date"],
                    snapshot.get("target_issue"),
                    snapshot["target_label"],
                    snapshot["generated_at"],
                    snapshot["model_version"],
                    snapshot["draw_count"],
                    json.dumps(snapshot["top_red_numbers"], ensure_ascii=False),
                    json.dumps(snapshot["top_blue_numbers"], ensure_ascii=False),
                    json.dumps(snapshot["tickets"], ensure_ascii=False),
                    json.dumps(snapshot["summary"], ensure_ascii=False),
                )
                for snapshot in snapshots
            ],
        )
        connection.commit()


def get_prediction_snapshot(
    base_issue: str,
    db_path: Path = DEFAULT_DB_PATH,
) -> dict[str, Any] | None:
    with get_connection(db_path) as connection:
        row = connection.execute(
            """
            SELECT
                id,
                base_issue,
                base_date,
                target_issue,
                target_label,
                generated_at,
                model_version,
                draw_count,
                top_red_numbers_json,
                top_blue_numbers_json,
                tickets_json,
                summary_json
            FROM prediction_snapshots
            WHERE base_issue = ?
            """,
            (base_issue,),
        ).fetchone()
    return _deserialize_prediction_row(row)


def get_latest_prediction_snapshot(
    db_path: Path = DEFAULT_DB_PATH,
) -> dict[str, Any] | None:
    with get_connection(db_path) as connection:
        row = connection.execute(
            """
            SELECT
                id,
                base_issue,
                base_date,
                target_issue,
                target_label,
                generated_at,
                model_version,
                draw_count,
                top_red_numbers_json,
                top_blue_numbers_json,
                tickets_json,
                summary_json
            FROM prediction_snapshots
            ORDER BY base_issue DESC, generated_at DESC, id DESC
            LIMIT 1
            """
        ).fetchone()
    return _deserialize_prediction_row(row)


def get_prediction_snapshot_index(
    db_path: Path = DEFAULT_DB_PATH,
) -> dict[str, dict[str, Any]]:
    with get_connection(db_path) as connection:
        rows = connection.execute(
            """
            SELECT
                id,
                base_issue,
                target_issue,
                model_version
            FROM prediction_snapshots
            """
        ).fetchall()
    return {
        row["base_issue"]: {
            "id": row["id"],
            "base_issue": row["base_issue"],
            "target_issue": row["target_issue"],
            "model_version": row["model_version"],
        }
        for row in rows
    }


def save_prediction_evaluation(
    evaluation: dict[str, Any],
    db_path: Path = DEFAULT_DB_PATH,
) -> None:
    save_prediction_evaluations_bulk([evaluation], db_path=db_path)


def save_prediction_evaluations_bulk(
    evaluations: list[dict[str, Any]],
    db_path: Path = DEFAULT_DB_PATH,
) -> None:
    if not evaluations:
        return
    with get_connection(db_path) as connection:
        connection.executemany(
            """
            INSERT INTO prediction_evaluations (
                snapshot_id,
                base_issue,
                target_issue,
                target_date,
                actual_red_numbers,
                actual_blue_number,
                evaluated_at,
                ticket_count,
                winning_ticket_count,
                winning_ticket_rate,
                highest_prize_level,
                prize_breakdown_json,
                ticket_results_json,
                model_version
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(snapshot_id) DO UPDATE SET
                base_issue = excluded.base_issue,
                target_issue = excluded.target_issue,
                target_date = excluded.target_date,
                actual_red_numbers = excluded.actual_red_numbers,
                actual_blue_number = excluded.actual_blue_number,
                evaluated_at = excluded.evaluated_at,
                ticket_count = excluded.ticket_count,
                winning_ticket_count = excluded.winning_ticket_count,
                winning_ticket_rate = excluded.winning_ticket_rate,
                highest_prize_level = excluded.highest_prize_level,
                prize_breakdown_json = excluded.prize_breakdown_json,
                ticket_results_json = excluded.ticket_results_json,
                model_version = excluded.model_version
            """,
            [
                (
                    evaluation["snapshot_id"],
                    evaluation["base_issue"],
                    evaluation["target_issue"],
                    evaluation["target_date"],
                    evaluation["actual_red_numbers"],
                    evaluation["actual_blue_number"],
                    evaluation["evaluated_at"],
                    evaluation["ticket_count"],
                    evaluation["winning_ticket_count"],
                    evaluation["winning_ticket_rate"],
                    evaluation["highest_prize_level"],
                    json.dumps(evaluation["prize_breakdown"], ensure_ascii=False),
                    json.dumps(evaluation["ticket_results"], ensure_ascii=False),
                    evaluation["model_version"],
                )
                for evaluation in evaluations
            ],
        )
        connection.commit()


def get_prediction_evaluation(
    base_issue: str,
    db_path: Path = DEFAULT_DB_PATH,
) -> dict[str, Any] | None:
    with get_connection(db_path) as connection:
        row = connection.execute(
            """
            SELECT
                id,
                snapshot_id,
                base_issue,
                target_issue,
                target_date,
                actual_red_numbers,
                actual_blue_number,
                evaluated_at,
                ticket_count,
                winning_ticket_count,
                winning_ticket_rate,
                highest_prize_level,
                prize_breakdown_json,
                ticket_results_json,
                model_version
            FROM prediction_evaluations
            WHERE base_issue = ?
            """,
            (base_issue,),
        ).fetchone()
    return _deserialize_evaluation_row(row)


def get_prediction_evaluation_index(
    db_path: Path = DEFAULT_DB_PATH,
) -> dict[str, dict[str, Any]]:
    with get_connection(db_path) as connection:
        rows = connection.execute(
            """
            SELECT
                snapshot_id,
                base_issue,
                model_version
            FROM prediction_evaluations
            """
        ).fetchall()
    return {
        row["base_issue"]: {
            "snapshot_id": row["snapshot_id"],
            "base_issue": row["base_issue"],
            "model_version": row["model_version"],
        }
        for row in rows
    }


def get_snapshots_ready_for_evaluation(
    db_path: Path = DEFAULT_DB_PATH,
) -> list[dict[str, Any]]:
    with get_connection(db_path) as connection:
        rows = connection.execute(
            """
            SELECT
                s.id,
                s.base_issue,
                s.base_date,
                s.target_issue,
                s.target_label,
                s.generated_at,
                s.model_version,
                s.draw_count,
                s.top_red_numbers_json,
                s.top_blue_numbers_json,
                s.tickets_json,
                s.summary_json,
                d.draw_date AS target_date,
                d.red_numbers AS target_red_numbers,
                d.blue_number AS target_blue_number
            FROM prediction_snapshots s
            INNER JOIN draws d
                ON d.issue = s.target_issue
            LEFT JOIN prediction_evaluations e
                ON e.snapshot_id = s.id
            WHERE e.id IS NULL
            ORDER BY s.base_issue ASC
            """
        ).fetchall()
    ready = []
    for row in rows:
        snapshot = _deserialize_prediction_row(row)
        snapshot["target_date"] = row["target_date"]
        snapshot["target_red_numbers"] = row["target_red_numbers"].split(",")
        snapshot["target_blue_number"] = row["target_blue_number"]
        ready.append(snapshot)
    return ready


def get_recent_prediction_evaluations(
    limit: int = 10,
    db_path: Path = DEFAULT_DB_PATH,
) -> list[dict[str, Any]]:
    with get_connection(db_path) as connection:
        rows = connection.execute(
            """
            SELECT
                id,
                snapshot_id,
                base_issue,
                target_issue,
                target_date,
                actual_red_numbers,
                actual_blue_number,
                evaluated_at,
                ticket_count,
                winning_ticket_count,
                winning_ticket_rate,
                highest_prize_level,
                prize_breakdown_json,
                ticket_results_json,
                model_version
            FROM prediction_evaluations
            ORDER BY target_issue DESC
            LIMIT ?
            """,
            (limit,),
        ).fetchall()
    return [_deserialize_evaluation_row(row) for row in rows]


def get_prediction_performance_summary(
    db_path: Path = DEFAULT_DB_PATH,
) -> dict[str, Any]:
    snapshots_total = get_prediction_snapshot_count(db_path=db_path)
    evaluated_total = get_prediction_evaluation_count(db_path=db_path)
    recent = get_recent_prediction_evaluations(limit=12, db_path=db_path)

    with get_connection(db_path) as connection:
        rows = connection.execute(
            """
            SELECT
                winning_ticket_count,
                ticket_count,
                highest_prize_level,
                prize_breakdown_json
            FROM prediction_evaluations
            """
        ).fetchall()

    total_tickets = 0
    winning_tickets = 0
    winning_issues = 0
    prize_breakdown_total = {
        "一等奖": 0,
        "二等奖": 0,
        "三等奖": 0,
        "四等奖": 0,
        "五等奖": 0,
        "六等奖": 0,
    }
    best_rank = None
    best_prize = None
    prize_rank = {
        "一等奖": 1,
        "二等奖": 2,
        "三等奖": 3,
        "四等奖": 4,
        "五等奖": 5,
        "六等奖": 6,
    }

    for row in rows:
        total_tickets += int(row["ticket_count"])
        winning_tickets += int(row["winning_ticket_count"])
        if int(row["winning_ticket_count"]) > 0:
            winning_issues += 1
        if row["highest_prize_level"]:
            current_rank = prize_rank[row["highest_prize_level"]]
            if best_rank is None or current_rank < best_rank:
                best_rank = current_rank
                best_prize = row["highest_prize_level"]
        breakdown = json.loads(row["prize_breakdown_json"])
        for prize_name, count in breakdown.items():
            prize_breakdown_total[prize_name] = prize_breakdown_total.get(prize_name, 0) + int(
                count
            )

    return {
        "snapshot_total": snapshots_total,
        "evaluated_total": evaluated_total,
        "pending_total": max(0, snapshots_total - evaluated_total),
        "issue_winning_total": winning_issues,
        "issue_win_rate_percent": round(
            winning_issues / evaluated_total * 100, 2
        )
        if evaluated_total
        else 0.0,
        "ticket_total": total_tickets,
        "ticket_winning_total": winning_tickets,
        "ticket_win_rate_percent": round(
            winning_tickets / total_tickets * 100, 2
        )
        if total_tickets
        else 0.0,
        "best_prize_level": best_prize,
        "prize_breakdown_total": prize_breakdown_total,
        "recent_evaluations": recent,
    }


def _deserialize_prediction_row(row: sqlite3.Row | None) -> dict[str, Any] | None:
    if row is None:
        return None
    return {
        "id": row["id"],
        "base_issue": row["base_issue"],
        "base_date": row["base_date"],
        "target_issue": row["target_issue"],
        "target_label": row["target_label"],
        "generated_at": row["generated_at"],
        "model_version": row["model_version"],
        "draw_count": row["draw_count"],
        "top_red_numbers": json.loads(row["top_red_numbers_json"]),
        "top_blue_numbers": json.loads(row["top_blue_numbers_json"]),
        "tickets": json.loads(row["tickets_json"]),
        "summary": json.loads(row["summary_json"]),
    }


def _deserialize_evaluation_row(row: sqlite3.Row | None) -> dict[str, Any] | None:
    if row is None:
        return None
    return {
        "id": row["id"],
        "snapshot_id": row["snapshot_id"],
        "base_issue": row["base_issue"],
        "target_issue": row["target_issue"],
        "target_date": row["target_date"],
        "actual_red_numbers": row["actual_red_numbers"].split(","),
        "actual_blue_number": row["actual_blue_number"],
        "evaluated_at": row["evaluated_at"],
        "ticket_count": row["ticket_count"],
        "winning_ticket_count": row["winning_ticket_count"],
        "winning_ticket_rate": row["winning_ticket_rate"],
        "highest_prize_level": row["highest_prize_level"],
        "prize_breakdown": json.loads(row["prize_breakdown_json"]),
        "ticket_results": json.loads(row["ticket_results_json"]),
        "model_version": row["model_version"],
    }
