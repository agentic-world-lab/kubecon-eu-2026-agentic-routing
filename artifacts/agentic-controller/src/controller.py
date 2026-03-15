import asyncio
import logging
import kopf

from .agent import process_llm_backend, reconcile_backend_manifests, delete_backend_manifests, delete_job

log = logging.getLogger(__name__)

# This handles the creation of a new LLMBackend
@kopf.on.create('edgecloudlabs.edgecloudlabs.com', 'v1alpha1', 'llmbackends')
def create_fn(spec, name, namespace, logger, **kwargs):
    logger.info(f"Detected new LLMBackend creation: {name} in {namespace}")
    
    model = spec.get("model", name)
    endpoint = spec.get("endpoint")
    deployment = spec.get("deployment", "local")
    
    if not endpoint:
        if deployment == "remote":
            # Default to direct OpenAI if remote and no endpoint provided
            endpoint = "https://api.openai.com/v1"
            logger.info(f"Using default gateway endpoint for remote deployment: {endpoint}")
        else:
            logger.error(f"Cannot process {name}: Missing 'endpoint' in spec for local deployment")
            return {"error": "Missing endpoint"}
        
    logger.info(f"Triggering Orchestrator agent for {name} (model={model}, deployment={deployment})")
    
    # Send the trigger to the agent logic asynchronously
    # Ensure this doesn't block the kopf thread
    try:
        # Since process_llm_backend is going to be a normal function making synchronous A2A calls,
        # we can just run it. If it was async, we'd need asyncio.run_coroutine_threadsafe.
        process_llm_backend(name=name, namespace=namespace, model=model, endpoint=endpoint, deployment=deployment)
        return {"status": "Evaluation Triggered"}
    except Exception as e:
        logger.error(f"Failed to trigger evaluation for {name}: {e}")
        raise kopf.TemporaryError(f"Evaluation failed: {e}", delay=30)

@kopf.on.update('edgecloudlabs.edgecloudlabs.com', 'v1alpha1', 'llmbackends')
def update_fn(spec, old, new, name, namespace, logger, **kwargs):
    if old and old.get('spec') == new.get('spec'):
        return

    logger.info(f"Detected LLMBackend update: {name} in {namespace}. Reconciling manifests.")
    model = spec.get("model", name)
    endpoint = spec.get("endpoint")
    deployment = spec.get("deployment", "local")
    
    if not endpoint and deployment == "local":
        return

    if not endpoint and deployment == "remote":
        # Default to direct OpenAI
        endpoint = "https://api.openai.com/v1"
        
    try:
        reconcile_backend_manifests(model=model, endpoint=endpoint, deployment=deployment, namespace=namespace)
        logger.info(f"Reconciliation successful for {name}")
    except Exception as e:
        logger.error(f"Failed to reconcile manifests for {name}: {e}")

@kopf.on.delete('edgecloudlabs.edgecloudlabs.com', 'v1alpha1', 'llmbackends')
def delete_fn(spec, name, namespace, logger, **kwargs):
    logger.info(f"Detected LLMBackend deletion: {name} in {namespace}. Cleaning up resources.")
    model = spec.get("model", name)
    try:
        delete_backend_manifests(model=model)
        delete_job(name=name, namespace=namespace)
        logger.info(f"Cleanup successful for {name}")
    except Exception as e:
        logger.error(f"Failed to cleanup manifests for {name}: {e}")
