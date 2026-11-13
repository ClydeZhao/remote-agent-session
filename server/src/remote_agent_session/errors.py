from enum import StrEnum

from pydantic import BaseModel


class RetryAction(StrEnum):
    same_request = "same_request"
    reconcile = "reconcile"
    user_action = "user_action"
    none = "none"


class ErrorBody(BaseModel):
    code: str
    message: str
    retry_action: RetryAction
    retry_after: int | None = None


class DomainError(Exception):
    def __init__(self, code: str, message: str, action: str = "none", status: int = 409):
        self.body = ErrorBody(code=code, message=message, retry_action=RetryAction(action))
        self.status = status


def missing():
    return DomainError("NOT_FOUND", "Resource not found.", "none", 404)
