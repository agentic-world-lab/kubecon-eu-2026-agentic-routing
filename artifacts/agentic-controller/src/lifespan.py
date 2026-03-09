import logging
import asyncio
import kopf
from contextlib import asynccontextmanager
from typing import Any

# Import controllers so kopf loads them
import src.controller  # noqa

@asynccontextmanager
async def lifespan(app: Any):
    logging.info("backend-evaluator-agent: starting up")
    
    # Run kopf operator in background
    loop = asyncio.get_running_loop()
    stop_flag = asyncio.Event()
    
    # Run kopf operator directly in the event loop
    kopf_task = asyncio.create_task(
        kopf.operator(
            namespaces=["intelligent-router-system"],
            stop_flag=stop_flag,
        )
    )
    
    try:
        yield
    finally:
        logging.info("backend-evaluator-agent: shutting down")
        stop_flag.set()
        await asyncio.gather(kopf_task, return_exceptions=True)
