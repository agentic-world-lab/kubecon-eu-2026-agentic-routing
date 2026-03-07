import logging
from contextlib import asynccontextmanager
from typing import Any


@asynccontextmanager
async def lifespan(app: Any):
    logging.info("backend-evaluator-agent: starting up")
    try:
        yield
    finally:
        logging.info("backend-evaluator-agent: shutting down")
