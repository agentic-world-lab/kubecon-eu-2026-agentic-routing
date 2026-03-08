"""
Backend Evaluator Agent — Pure A2A Orchestrator.

Orchestrates the LLMBackend evaluation lifecycle by calling other agents
via A2A. No direct Kubernetes interaction.

A2A targets:
  - K8s Agent (future)          → create backend, launch job, collect results, update status
  - model-cost-agent            → token pricing from OpenRouter
  - sp-electricity-cost-agent   → electricity cost from ESIOS
"""

import json
import logging
import os
import time
import yaml

from google.adk import Agent
from google.adk.tools.tool_context import ToolContext
from kubernetes import client, config

from .a2a_client import call_agent

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
K8S_AGENT_NAME: str = os.getenv("K8S_AGENT_NAME", "k8s-agent")
COST_AGENT_NAME: str = os.getenv("COST_AGENT_NAME", "model-cost-agent")
ENERGY_AGENT_NAME: str = os.getenv("ENERGY_AGENT_NAME", "sp-electricity-cost-agent")
SR_CONFIG_AGENT_NAME: str = os.getenv("SR_CONFIG_AGENT_NAME", "sr-config-agent")

# ---------------------------------------------------------------------------
# Tools — K8s operations (via A2A to future K8s Agent)
# ---------------------------------------------------------------------------

def create_backend(
    model: str,
    endpoint: str,
    api_key_secret: str,
    namespace: str,
    deployment: str,
    tool_context: ToolContext,
) -> str:
    """Create an AgentgatewayBackend and HTTPRoute for the given model.

    Delegates to the K8s Agent via A2A.

    Args:
        model: The model name/identifier (e.g. 'gpt-4o').
        endpoint: The model endpoint URL (e.g. 'http://10.0.0.1:8000/v1').
        api_key_secret: Name of the K8s Secret containing the API key.
        namespace: Kubernetes namespace.
        deployment: 'local' or 'remote'.
        tool_context: ToolContext instance.

    Returns:
        A string confirming the backend and route were created.
    """
    import urllib.parse
    backend_name = model.replace(".", "-")

    agentgateway_spec = {
        "ai": {
            "provider": {
                "openai": {
                    "model": model
                }
            }
        }
    }

    if deployment == "local":
        parsed = urllib.parse.urlparse(endpoint)
        agentgateway_spec["ai"]["provider"]["host"] = parsed.hostname or ""
        agentgateway_spec["ai"]["provider"]["port"] = parsed.port or (443 if parsed.scheme == "https" else 80)
        agentgateway_spec["ai"]["provider"]["path"] = parsed.path or "/v1/chat/completions"
    else:
        agentgateway_spec["policies"] = {
            "auth": {
                "secretRef": {
                    "name": api_key_secret
                }
            }
        }

    manifests = [
        {
            "apiVersion": "gateway.networking.k8s.io/v1",
            "kind": "HTTPRoute",
            "metadata": {
                "name": f"multi-model-{backend_name}",
                "namespace": "agentgateway-system"
            },
            "spec": {
                "parentRefs": [
                    {
                        "name": "agentgateway-proxy",
                        "namespace": "agentgateway-system"
                    }
                ],
                "rules": [
                    {
                        "matches": [
                            {
                                "headers": [
                                    {
                                        "type": "Exact",
                                        "name": "x-router-selected-model",
                                        "value": model
                                    }
                                ]
                            }
                        ],
                        "backendRefs": [
                            {
                                "name": backend_name,
                                "namespace": "agentgateway-system",
                                "group": "agentgateway.dev",
                                "kind": "AgentgatewayBackend"
                            }
                        ]
                    }
                ]
            }
        },
        {
            "apiVersion": "agentgateway.dev/v1alpha1",
            "kind": "AgentgatewayBackend",
            "metadata": {
                "name": backend_name,
                "namespace": "agentgateway-system"
            },
            "spec": agentgateway_spec
        }
    ]
    
    yaml_docs = [yaml.dump(m, sort_keys=False) for m in manifests]
    yaml_string = "\n---\n".join(yaml_docs)
    
    message = (
        f"You are the K8s Agent. Execute tools to create the following two resources exactly as provided via YAML:\n\n"
        f"---\n{yaml_string}\n---\n\n"
        f"Use your tools to apply this YAML exactly. Do not guess namespaces. Return success or failure."
    )
    result = call_agent(K8S_AGENT_NAME, message)
    if tool_context:
        tool_context.state["last_backend_created"] = model
    return result

def delete_backend_manifests(model: str) -> str:
    """Delete the AgentgatewayBackend and HTTPRoute via A2A."""
    backend_name = model.replace(".", "-")
    message = (
        f"You are the K8s Agent. Please delete the HTTPRoute named 'multi-model-{backend_name}' "
        f"and the AgentgatewayBackend named '{backend_name}' in the 'agentgateway-system' namespace. "
        f"If they do not exist, just report success."
    )
    return call_agent(K8S_AGENT_NAME, message)

def reconcile_backend_manifests(model: str, endpoint: str, deployment: str, namespace: str) -> str:
    """Re-apply manifests if the LLMBackend is updated, without triggering a full evaluation."""
    return create_backend(model, endpoint, "openai-secret", namespace, deployment, None)

def delete_job(name: str, namespace: str) -> str:
    """Delete the evaluation Job via A2A."""
    job_name = f"eval-{name}"
    message = (
        f"You are the K8s Agent. Please delete the Job named '{job_name}' "
        f"in the '{namespace}' namespace. If it does not exist, just report success."
    )
    return call_agent(K8S_AGENT_NAME, message)


def launch_eval_job(
    name: str,
    model_name_from_spec: str,
    endpoint: str,
    namespace: str,
    tool_context: ToolContext,
) -> str:
    logging.info(f"launch_eval_job: name={name}, model_name_from_spec={model_name_from_spec}, endpoint={endpoint}")
    """Launch an MMLU evaluation Kubernetes Job for the given model.

    Delegates to the K8s Agent via A2A.

    Args:
        name: The LLMBackend resource name.
        model_name_from_spec: The model name from 'spec.model'.
        endpoint: The model endpoint URL.
        namespace: Kubernetes namespace.

    Returns:
        A string confirming the job was created.
    """
    # Use a deterministic name to prevent double-triggering from multiple controllers.
    # If the job already exists, the K8s Agent tool will return an error, 
    # and the second orchestrator will fail/stop harmlessly.
    job_name = f"eval-{name}"

    job_manifest = {
        "apiVersion": "batch/v1",
        "kind": "Job",
        "metadata": {
            "name": job_name,
            "namespace": namespace
        },
        "spec": {
            "backoffLimit": 0,
            "template": {
                "spec": {
                    "serviceAccountName": "default",
                    "containers": [
                        {
                            "name": "eval",
                            "image": "fjvicens/mmlu-pro-eval-job:0.2",
                            "env": [
                                {
                                    "name": "EVAL_MODEL",
                                    "value": model_name_from_spec
                                },
                                {
                                    "name": "EVAL_ENDPOINT",
                                    "value": endpoint
                                },
                                {
                                    "name": "OPENAI_API_KEY",
                                    "valueFrom": {
                                        "secretKeyRef": {
                                            "name": "openai-secret",
                                            "key": "OPENAI_API_KEY"
                                        }
                                    }
                                }
                            ]
                        }
                    ],
                    "restartPolicy": "Never"
                }
            }
        }
    }
    
    yaml_string = yaml.dump(job_manifest, sort_keys=False)

    message = (
        f"You are the K8s Agent. Execute tools to create the following evaluation Job exactly as provided via YAML:\n\n"
        f"---\n{yaml_string}\n---\n\n"
        "Use your tools to apply this YAML exactly. Return success or failure."
    )
    result = call_agent(K8S_AGENT_NAME, message)
    tool_context.state["last_job_launched"] = job_name
    return result


def collect_results(
    job_name: str,
    namespace: str,
    tool_context: ToolContext,
) -> str:
    """Collect evaluation results from a completed MMLU job using direct K8s API.

    Args:
        job_name: Name of the evaluation job.
        namespace: Kubernetes namespace.
        tool_context: Context for state.

    Returns:
        JSON string with evaluation results.
    """
    try:
        config.load_incluster_config()
        core_api = client.CoreV1Api()
        
        # Find the pod for the job
        pods = core_api.list_namespaced_pod(
            namespace, 
            label_selector=f"job-name={job_name}"
        )
        
        if not pods.items:
            raise Exception(f"No pods found for job {job_name}")
            
        pod_name = pods.items[0].metadata.name
        log.info(f"Extracting results from pod {pod_name} logs")
        
        pod_logs = core_api.read_namespaced_pod_log(pod_name, namespace)
        
        # Look for the last JSON object containing our metrics
        # The MMLU eval script prints a final JSON block
        import re
        # re.DOTALL (or (?s)) allows . to match newlines
        # Greedy match from the first { that eventually contains overall_accuracy to the last }
        json_matches = re.findall(r'(\{.*"overall_accuracy".*\})', pod_logs, re.DOTALL)
            
        if json_matches:
            # Take the last match and ensure it's valid JSON
            return json_matches[-1]
            
        return pod_logs # Fallback
        
    except Exception as e:
        log.warning(f"Direct log collection failed: {e}. Falling back to A2A.")
        message = (
            f"You are the K8s Agent. Please get the logs of the pod associated with Job '{job_name}' in namespace '{namespace}'.\n"
            f"Use your 'k8s_get_pod_logs' tool. Extract the final JSON result from the logs containing "
            f"overall_accuracy, tok/s, and avg_response_time."
        )
        result = call_agent(K8S_AGENT_NAME, message)
        tool_context.state["last_results"] = result
        return result


def check_job_status(
    job_name: str,
    namespace: str,
) -> str:
    """Check the status of a Kubernetes Job using direct K8s API.

    Args:
        job_name: Name of the job.
        namespace: Kubernetes namespace.

    Returns:
        The job status (Succeeded, Failed, Running, or Not Found).
    """
    try:
        config.load_incluster_config()
        batch_api = client.BatchV1Api()
        
        # Using read_namespaced_job instead of read_namespaced_job_status
        # as the latter requires extra RBAC and the former includes status anyway.
        job = batch_api.read_namespaced_job(job_name, namespace)
        
        if job.status.succeeded and job.status.succeeded >= 1:
            return "Succeeded"
        if job.status.failed and job.status.failed >= 1:
            return "Failed"
        return "Running"
        
    except Exception as e:
        log.warning(f"Direct check_job_status failed for {job_name}: {e}. Falling back to A2A.")
        message = f"Check the status of Job '{job_name}' in namespace '{namespace}'. Tell me if it has Succeeded, Failed, or is still Running."
        return call_agent(K8S_AGENT_NAME, message)


def update_cr_status(
    name: str,
    namespace: str,
    phase: str,
    results: str,
    tool_context: ToolContext,
) -> str:
    """Update the LLMBackend CR status with phase and results.

    Delegates to the K8s Agent via A2A.

    Args:
        name: The LLMBackend resource name.
        namespace: Kubernetes namespace.
        phase: New phase value (BackendCreated, Evaluating, Evaluated, Failed).
        results: JSON string with all results to store in status (can be empty).

    Returns:
        A string confirming the status was updated.
    """
    try:
        # Load in-cluster config (Orchestrator pod has SA with permissions)
        config.load_incluster_config()
        custom_api = client.CustomObjectsApi()
        
        parsed_results = json.loads(results) if isinstance(results, str) and results.strip() else {}
        
        patch_body = {
            "status": {
                "phase": phase,
                "results": parsed_results
            }
        }
        
        log.info(f"Direct patching {name} in {namespace} to phase {phase}")
        
        custom_api.patch_namespaced_custom_object_status(
            group="edgecloudlabs.edgecloudlabs.com",
            version="v1alpha1",
            namespace=namespace,
            plural="llmbackends",
            name=name,
            body=patch_body
        )
        
        if tool_context:
            tool_context.state["last_phase"] = phase
            
        return f"Status updated to '{phase}' successfully via direct K8s API."
        
    except Exception as e:
        log.warning(f"Direct status patch failed for {name}: {e}. Falling back to K8s Agent A2A instruction.")
        
        try:
            a2a_results = json.loads(results) if isinstance(results, str) and results.strip() else {}
        except:
            a2a_results = {}

        message = (
            f"You are the K8s Agent. I attempted a direct patch on the 'status' subresource of the LLMBackend '{name}' "
            f"in namespace '{namespace}' but it failed (error: {str(e)}). Please attempt to set its 'status.phase' to '{phase}' and "
            f"'status.results' to {json.dumps(a2a_results)} using your most reliable tool. "
            f"If you use a patch tool, remember that CRD status requires 'merge' or 'json-patch' strategy and subresource targeting."
        )
        
        result = call_agent(K8S_AGENT_NAME, message)
        if tool_context:
            tool_context.state["last_phase"] = phase
        return f"A2A Fallback result: {result}"


# ---------------------------------------------------------------------------
# Tools — External data (via A2A to existing kagent agents)
# ---------------------------------------------------------------------------

def get_model_pricing(model: str) -> str:
    """Get token pricing for a model from the model-cost-agent via A2A.

    Calls the model-cost-agent which uses the OpenRouter API to retrieve
    prompt and completion costs per 1M tokens.

    Args:
        model: The model name/identifier (e.g. 'gpt-4o').

    Returns:
        JSON string with pricing info, e.g.:
        '{"model": "gpt-4o", "pricing": [{"prompt": "2.50", "completion": "10.00"}]}'
    """
    message = f"Get the pricing for model {model}"
    return call_agent(COST_AGENT_NAME, message)


def get_energy_cost() -> str:
    """Get the current electricity price in Spain from the sp-electricity-cost-agent via A2A.

    Calls the sp-electricity-cost-agent which uses the ESIOS API to retrieve
    the current PVPC electricity price in €/kWh.

    Returns:
        JSON string with energy cost, e.g.: '{"price": "0.007"}'
    """
    message = "Get the current electricity price in Spain"
    return call_agent(ENERGY_AGENT_NAME, message)


# ---------------------------------------------------------------------------
# Tools — Configuration Loop
# ---------------------------------------------------------------------------

def trigger_router_config_update(tool_context: ToolContext) -> str:
    """Trigger the sr-config-agent to update the Semantic Router configuration based on the latest evaluations."""
    message = "Trigger the semantic router configuration update based on the latest benchmark results."
    result = call_agent(SR_CONFIG_AGENT_NAME, message)
    tool_context.state["last_config_update"] = result
    return result


# ---------------------------------------------------------------------------
# Agent Definition
# ---------------------------------------------------------------------------

root_agent = Agent(
    model="openai/gpt-4.1-mini",
    name="backend_evaluator_agent",
    description=(
        "Orchestrates LLM backend evaluation lifecycle. "
        "Creates backends, triggers evaluations, collects results, "
        "and enriches them with pricing and energy cost data."
    ),
    instruction="""\
You are the Backend Evaluator Agent. You orchestrate the lifecycle of LLM
backend evaluations by calling other agents via A2A.

## Phase-Driven Workflow

When you receive a message about an LLMBackend resource, carefully check:
- `metadata.name`: Use this for the `name` argument in all tools.
- `spec.model`: Use this for `model_name_from_spec` in `launch_eval_job` and for `model` in `get_model_pricing`.
- `spec.endpoint`: Use this for `endpoint`.

When you receive a message about an LLMBackend resource, follow this workflow
based on the current phase:

### Phase "" (empty / new resource)
1. Call `create_backend` with the model details to create the AgentgatewayBackend
   and HTTPRoute.
2. Call `update_cr_status` with phase="BackendCreated".

### Phase "BackendCreated"
1. Call `launch_eval_job` using `spec.model` for the `model_name_from_spec` argument.
2. Call `update_cr_status` with phase="Evaluating".

### Phase "Evaluating"
1. Call `check_job_status` for the job you launched (found in your state or by listing jobs).
2. If the job status is "Succeeded":
   - Proceed to "Evaluation Complete" steps.
3. If the job status is "Failed":
   - Call `update_cr_status` with phase="Failed" and include the failure reason in results.
4. If the job is still "Running":
   - Do not call any other tools for this resource. Report that you are waiting for the job to complete.

### Evaluation Complete (Results Ready)
1. Call `collect_results` for the successful job.
2. Check `spec.deployment` of the LLMBackend resource:
   - If it is "local": Call `get_energy_cost`.
   - If it is "remote": Call `get_model_pricing` using `spec.model` from the resource.
3. Combine all data into the final results JSON. Ensure you include:
   - Evaluation metrics: accuracy, tok/s, latency (from `collect_results`).
   - Pricing: If "remote", use data from `get_model_pricing`.
   - Energy: If "local", use data from `get_energy_cost`.
4. Call `update_cr_status` with phase="Evaluated" and the combined results.

The enriched results JSON should have this structure (include only relevant cost fields):
{
  "overallAccuracy": "...",
  "tokPerSecond": "...",
  "avgResponseTime": "...",
  "pricing": { // Only if remote
    "promptCostPer1M": "...",
    "completionCostPer1M": "..."
  },
  "energyCost": { // Only if local
    "pricePerKwh": "..."
  }
}

### Error Handling
- If any tool call fails, call `update_cr_status` with phase="Failed" and
  include the error message in results.
- Always report what happened clearly in your response.

## Important Rules
- You MUST call tools to perform actions. Never make up results.
- Follow the phase workflow strictly. Do not skip phases.
- When combining results, preserve exact numeric values from tool responses.
""",
    tools=[
        create_backend,
        launch_eval_job,
        check_job_status,
        collect_results,
        update_cr_status,
        get_model_pricing,
        get_energy_cost,
    ],
)

def adapt_llm_price(
    p_in_api,
    p_out_api,
    elec_local,
    elec_public=0.1,
    margin_share=0.2,
    electricity_share=0.2,
):
    """
    Adapts public API prices to local deployment based on electricity costs.
    Formula from local_price_adaptation.md
    """
    try:
        elec_public = elec_local * 1.2        
        p_in_api = float(p_in_api)
        p_out_api = float(p_out_api)
        elec_local = float(elec_local)
        
        if p_in_api <= 0: return 0.0, 0.0
        
        r = p_out_api / p_in_api

        p_avg = (p_in_api + p_out_api) / 2

        # remove margin
        p_no_margin = p_avg * (1 - margin_share)

        # split infra vs electricity
        p_elec_api = p_no_margin * electricity_share
        p_infra = p_no_margin * (1 - electricity_share)

        # replace electricity
        p_elec_local = p_elec_api * (elec_local / elec_public)

        p_avg_local = p_infra + p_elec_local

        p_in_local = (2 * p_avg_local) / (1 + r)
        p_out_local = r * p_in_local

        return p_in_local, p_out_local
    except Exception as e:
        log.error(f"Error in adapt_llm_price: {e}")
        return 0.0, 0.0

def process_llm_backend(name: str, namespace: str, model: str, endpoint: str, deployment: str):
    """
    Called by the Kopf controller when an LLMBackend is created.
    This drives the evaluation logic synchronously through A2A calls.
    In a real agentic setup, you might trigger the agent itself with a free-text prompt,
    but here we deterministically call the tools to emulate the workflow.
    """
    class DummyContext:
        def __init__(self):
            self.state = {}
            
    ctx = DummyContext()
    
    log.info(f"process_llm_backend: Starting workflow for {name} (model={model})")
    try:
        # Phase "": create backend
        create_backend(model, endpoint, "openai-secret", namespace, deployment, ctx)
        update_cr_status(name, namespace, "BackendCreated", "", ctx)
        
        # Phase "BackendCreated": launch job
        launch_eval_job(name, model, endpoint, namespace, ctx)
        update_cr_status(name, namespace, "Evaluating", "", ctx)
        log.info(f"process_llm_backend: Job launched for {name}. Waiting for completion.")
        
        # Wait for Job to be Succeeded or Failed
        max_attempts = 60
        job_name = ctx.state.get("last_job_launched", f"{name}-job")
        job_succeeded = False
        
        for _ in range(max_attempts):
            status = check_job_status(job_name, namespace)
            if status == "Succeeded":
                job_succeeded = True
                break
            elif status == "Failed":
                break
            time.sleep(10)
            
        if not job_succeeded:
            update_cr_status(name, namespace, "Failed", '{"error": "Job failed or timed out"}', ctx)
            return

        # Phase "Evaluation Complete"
        raw_results = collect_results(job_name, namespace, ctx)
        log.info(f"raw_results collected: {len(raw_results) if raw_results else 0} chars")
        
        job_data = {}
        try:
            if raw_results:
                job_data = json.loads(raw_results)
        except Exception as e:
            log.warning(f"Could not parse collect_results JSON: {e}")
            
        # Map snake_case from job to camelCase for CRD columns
        def safe_float(v, default=0.0):
            try: return float(v)
            except: return default

        acc = safe_float(job_data.get("overall_accuracy", 0))
        lat = safe_float(job_data.get("avg_response_time", 0))

        final_results = {
            "overallAccuracy": f"{acc:.2f}" if acc else "0.00",
            "avgResponseTime": f"{lat:.2f}" if lat else "",
            "tokensPerSecond": f"{safe_float(job_data.get('tok/s', 0)):.2f}",
            "categoryAccuracy": {k: f"{safe_float(v):.2f}" for k, v in job_data.get("category_accuracy", {}).items()}
        }
        
        if deployment == "local":
            energy_response = get_energy_cost()
            elec_local = 0.07 # fallback
            try:
                e_data = json.loads(energy_response)
                final_results["energyCost"] = e_data
                elec_local = safe_float(e_data.get("price", 0.07))
            except Exception as e:
                log.warning(f"Failed to parse energy cost: {e}")

            # Get reference API pricing for adaptation
            pricing_response = get_model_pricing(model)
            try:
                p_data = json.loads(pricing_response)
                # Fallback if model not found
                if "Error" in pricing_response or "error" in p_data or not p_data.get("pricing"):
                    log.info(f"Reference pricing for {model} not found, falling back to gpt-4o")
                    pricing_response = get_model_pricing("gpt-4o")
                    p_data = json.loads(pricing_response)

                if "pricing" in p_data and len(p_data["pricing"]) > 0:
                    ref = p_data["pricing"][0]
                    p_in_api = safe_float(ref.get("prompt", 0))
                    p_out_api = safe_float(ref.get("completion", 0))
                    
                    p_in_local, p_out_local = adapt_llm_price(p_in_api, p_out_api, elec_local)
                    final_results["pricing"] = {
                        "prompt": f"{p_in_local:.2f}",
                        "completion": f"{p_out_local:.2f}"
                    }
                else:
                    final_results["pricing"] = {"prompt": "N/A", "completion": "N/A"}
            except Exception as e:
                log.warning(f"Failed to adapt pricing: {e}")
                final_results["pricing"] = {"prompt": "Error", "completion": "Error"}
        else:
            pricing_response = get_model_pricing(model)
            try:
                # Based on docstring, returns {"model": "...", "pricing": [{"prompt": "...", "completion": "..."}]}
                p_data = json.loads(pricing_response)
                if "pricing" in p_data and isinstance(p_data["pricing"], list) and len(p_data["pricing"]) > 0:
                    item = p_data["pricing"][0]
                    final_results["pricing"] = {
                        "prompt": str(item.get("prompt", "")),
                        "completion": str(item.get("completion", ""))
                    }
                else:
                    final_results["pricing"] = {"prompt": "N/A", "completion": "N/A"}
            except Exception as e:
                log.warning(f"Failed to parse pricing: {e}")
                
        # Final update
        update_cr_status(name, namespace, "Evaluated", json.dumps(final_results), ctx)
        log.info(f"process_llm_backend: Completed workflow for {name}")

        # Automatic Job Cleanup: Delete the job after results are persistent in the CRD
        try:
            log.info(f"process_llm_backend: Deleting completed evaluation job {job_name}")
            delete_job(name, namespace)
        except Exception as cleanup_err:
            log.warning(f"Failed to auto-delete job {job_name}: {cleanup_err}")
        
    except Exception as e:
        log.error(f"Error in process_llm_backend for {name}: {e}")
        try:
            update_cr_status(name, namespace, "Failed", json.dumps({"error": str(e)}), ctx)
        except:
            pass
