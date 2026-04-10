from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator


DEFAULT_EXCLUDE_TYPES = [
    "follow",
    "follow_request",
    "update",
    "admin.sign_up",
    "admin.report",
]


class Config(BaseModel):
    model_config = ConfigDict(extra="ignore")

    mastodon_enabled: bool = True
    mastodon_transport: Literal["streaming", "polling"] = "streaming"
    mastodon_base_url: str = ""
    mastodon_access_token: str = ""
    mastodon_streaming_url: str = ""
    mastodon_stream_reconnect_delay: int = 5
    mastodon_stream_ping_interval: float = 20.0
    mastodon_stream_ping_timeout: float = 20.0
    mastodon_check_interval: int = 60
    mastodon_timeout: float = 15.0
    mastodon_limit: int = 80
    mastodon_include_filtered: bool = False
    mastodon_exclude_types: list[str] = Field(
        default_factory=lambda: DEFAULT_EXCLUDE_TYPES.copy()
    )
    mastodon_notify_private_ids: list[int] = Field(default_factory=list)
    mastodon_notify_group_ids: list[int] = Field(default_factory=list)
    mastodon_onebot_self_id: str | None = None
    mastodon_state_file: str = "data/mastodon_message_state.json"
    mastodon_init_skip_history: bool = True
    mastodon_preview_length: int = 160
    mastodon_message_max_length: int = 1600

    @field_validator("mastodon_base_url")
    @classmethod
    def normalize_base_url(cls, value: str) -> str:
        return value.strip().rstrip("/")

    @field_validator("mastodon_streaming_url")
    @classmethod
    def normalize_streaming_url(cls, value: str) -> str:
        return value.strip().rstrip("/")

    @field_validator("mastodon_onebot_self_id")
    @classmethod
    def normalize_self_id(cls, value: str | None) -> str | None:
        if value is None:
            return None
        value = value.strip()
        return value or None

    @field_validator("mastodon_check_interval")
    @classmethod
    def validate_check_interval(cls, value: int) -> int:
        if value <= 0:
            raise ValueError("mastodon_check_interval must be greater than 0")
        return value

    @field_validator("mastodon_stream_reconnect_delay")
    @classmethod
    def validate_stream_reconnect_delay(cls, value: int) -> int:
        if value <= 0:
            raise ValueError("mastodon_stream_reconnect_delay must be greater than 0")
        return value

    @field_validator("mastodon_stream_ping_interval", "mastodon_stream_ping_timeout")
    @classmethod
    def validate_stream_ping(cls, value: float) -> float:
        if value <= 0:
            raise ValueError("stream ping values must be greater than 0")
        return value

    @field_validator("mastodon_timeout")
    @classmethod
    def validate_timeout(cls, value: float) -> float:
        if value <= 0:
            raise ValueError("mastodon_timeout must be greater than 0")
        return value

    @field_validator("mastodon_limit")
    @classmethod
    def validate_limit(cls, value: int) -> int:
        if value <= 0 or value > 80:
            raise ValueError("mastodon_limit must be between 1 and 80")
        return value

    @field_validator("mastodon_preview_length")
    @classmethod
    def validate_preview_length(cls, value: int) -> int:
        if value < 20:
            raise ValueError("mastodon_preview_length must be at least 20")
        return value

    @field_validator("mastodon_message_max_length")
    @classmethod
    def validate_message_max_length(cls, value: int) -> int:
        if value < 200:
            raise ValueError("mastodon_message_max_length must be at least 200")
        return value

    @property
    def has_targets(self) -> bool:
        return bool(self.mastodon_notify_private_ids or self.mastodon_notify_group_ids)

    @property
    def is_ready(self) -> bool:
        return bool(
            self.mastodon_enabled
            and self.mastodon_base_url
            and self.mastodon_access_token
            and self.has_targets
        )
