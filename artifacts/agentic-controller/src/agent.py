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

from google.adk import Agent
from google.adk.tools.tool_context import ToolContext

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
    tool_context: ToolContext,
) -> str:
    """Create an AgentgatewayBackend and HTTPRoute for the given model.

    Delegates to the K8s Agent via A2A.

    Args:
        model: The model name/identifier (e.g. 'gpt-4o').
        endpoint: The model endpoint URL (e.g. 'http://10.0.0.1:8000/v1').
        api_key_secret: Name of the K8s Secret containing the API key.
        namespace: Kubernetes namespace.

    Returns:
        A string confirming the backend and route were created.
    """
    message = (
        f"You are the K8s Agent. Execute tools to create the following two resources exactly as provided via YAML:\n\n"
        f"---\n"
        f"apiVersion: agentgateway.dev/v1alpha1\n"
        f"kind: AgentgatewayBackend\n"
        f"metadata:\n"
        f"  name: {model}\n"
        f"  namespace: {namespace}\n"
        f"spec:\n"
        f"  ai:\n"
        f"    provider:\n"
        f"      openai:\n"
        f"        model: {model}\n"
        f"  policies:\n"
        f"    auth:\n"
        f"      secretRef:\n"
        f"        name: {api_key_secret}\n"
        f"---\n"
        f"apiVersion: gateway.networking.k8s.io/v1\n"
        f"kind: HTTPRoute\n"
        f"metadata:\n"
        f"  name: {model}-route\n"
        f"  namespace: {namespace}\n"
        f"spec:\n"
        f"  parentRefs:\n"
        f"    - name: agentgateway-proxy\n"
        f"      namespace: agentgateway-system\n"
        f"  rules:\n"
        f"  - matches:\n"
        f"    - path:\n"
        f"        type: PathPrefix\n"
        f"        value: /v1\n"
        f"      headers:\n"
        f"      - type: Exact\n"
        f"        name: x-vsr-selected-model\n"
        f"        value: \"{model}\"\n"
        f"    backendRefs:\n"
        f"    - name: {model}\n"
        f"      namespace: {namespace}\n"
        f"      group: agentgateway.dev\n"
        f"      kind: AgentgatewayBackend\n\n"
        f"Use your tools to apply this YAML exactly. Do not guess namespaces. Return success or failure."
    )
    result = call_agent(K8S_AGENT_NAME, message)
    tool_context.state["last_backend_created"] = model
    return result


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
    # Create a unique job name to avoid "field is immutable" errors on re-runs
    job_name = f"{name}-{int(time.time())}"

    message = (
        f"You are the K8s Agent. Execute tools to create the following evaluation Job exactly as provided via YAML:\n\n"
        f"---\n"
        f"apiVersion: batch/v1\n"
        f"kind: Job\n"
        f"metadata:\n"
        f"  name: {job_name}\n"
        f"  namespace: {namespace}\n"
        f"spec:\n"
        f"  backoffLimit: 0\n"
        f"  template:\n"
        f"    spec:\n"
        f"      containers:\n"
        f"      - name: eval\n"
        f"        image: fjvicens/mmlu-pro-eval-job:0.2\n"
        f"        env:\n"
        f"        - name: EVAL_MODEL\n"
        f"          value: \"{model_name_from_spec}\"\n"
        f"        - name: EVAL_ENDPOINT\n"
        f"          value: \"{endpoint}\"\n"
        f"        - name: OPENAI_API_KEY\n"
        f"          valueFrom:\n"
        f"            secretKeyRef:\n"
        f"              name: kagent-openai\n"
        f"              key: OPENAI_API_KEY\n"
        f"      restartPolicy: Never\n"
        f"---\n\n"
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
    """Collect evaluation results from a completed MMLU job.

    Delegates to the K8s Agent via A2A.

    Args:
        job_name: Name of the evaluation job.
        namespace: Kubernetes namespace.

    Returns:
        JSON string with evaluation results (accuracy, tok/s, latency).
    """
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
    """Check the status of a Kubernetes Job.

    Args:
        job_name: Name of the job.
        namespace: Kubernetes namespace.

    Returns:
        The job status (Succeeded, Failed, Running, or Not Found).
    """
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
        parsed_results = json.loads(results) if isinstance(results, str) and results.strip() else {}
    except ValueError:
        parsed_results = {}
        
    # Wrap in "status" as confirmed by user testing
    patch_payload = {
        "status": {
            "phase": phase,
            "results": parsed_results
        }
    }

    message = (
        f"You are the K8s Agent. You must patch the LLMBackend '{name}' status in namespace '{namespace}' to '{phase}'.\n\n"
        f"Use your 'k8s_patch_status' tool with these arguments:\n"
        f"  name: {name}\n"
        f"  namespace: {namespace}\n"
        f"  kind: LLMBackend\n"
        f"  patch: {json.dumps(patch_payload)}\n\n"
        f"Complete the task and report success."
    )
    result = call_agent(K8S_AGENT_NAME, message)
    tool_context.state["last_phase"] = phase
    return result


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
        trigger_router_config_update,
    ],
)
