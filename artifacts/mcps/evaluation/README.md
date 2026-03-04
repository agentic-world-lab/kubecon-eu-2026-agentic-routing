# MMLU-Pro Evaluation Job

This project provides a containerized tool designed to evaluate AI models using the [MMLU-Pro](https://huggingface.co/datasets/TIGER-Lab/MMLU-Pro) benchmark. It is specifically optimized for execution as a Kubernetes Job, enabling scalable and reproducible performance measurement within a cluster.

## Purpose

The primary goal of this tool is to provide a standardized way to benchmark AI models against a rigorous set of multiple-choice questions across various categories. Unlike standard evaluations, this implementation:
- Tracks **Tokens per Second (tok/s)** for each request.
- Calculates **Overall Accuracy** and **Average Response Time**.
- Delivers results in a **standardized JSON format** via stdout for easy integration with downstream monitoring or reporting tools.

## Why an Evaluation Job?

Running evaluations as Kubernetes Jobs offers several advantages:
- **Reproducibility**: Ensures that every model is tested in a consistent, isolated environment.
- **Resource Management**: Leverages Kubernetes scheduling to run evaluations when resources are available without interfering with other services.
- **Automation**: Can be easily integrated into CI/CD pipelines to automatically benchmark new model versions or configurations.

## How it works

The evaluation job is typically triggered by applying a Kubernetes Job manifest. 

### Triggering the Job

1.  **Configure the manifest**: Edit `mmlu-eval-job.yaml` to set your desired model and endpoint.
2.  **Apply the manifest**:
    ```bash
    kubectl apply -f mmlu-eval-job.yaml
    ```
3.  **Retrieve results**: Once the job is finished, check the logs of the pod:
    ```bash
    kubectl logs job/mmlu-eval-job
    ```

### Configuration

The job configuration is handled via environment variables:
- `EVAL_ENDPOINT`: The OpenAI-compatible API endpoint (defaults to `https://api.openai.com/v1`).
- `EVAL_MODEL`: The name of the model to evaluate.
- `OPENAI_API_KEY`: The API key for the endpoint.

## Building the Container

To build the evaluation tool container locally:

```bash
docker build -t <your-registry>/mmlu-pro-eval-job:<version> .
```

To push the image to a registry:

```bash
docker push <your-registry>/mmlu-pro-eval-job:<version>
```

## Output Format

The tool outputs a single JSON object to stdout upon completion:

```json
{
  "model": "gpt-oss-120b",
  "overall_accuracy": 0.6714285714285714,
  "avg_response_time": 4.9934315000261575,
  "tok/s": 200,
  "category_accuracy": {
    "business": 0.6,
    "law": 0.6,
    ...
  }
}
```

Diagnostic information and progress bars are emitted to **stderr** to ensure they do not contaminate the JSON output.
