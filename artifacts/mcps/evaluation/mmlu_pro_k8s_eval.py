# MMLU-Pro evaluation script for Kubernetes
# Based on mmlu_pro_vllm_eval.py

import argparse
import json
import os
import random
import re
import sys
import time
from concurrent.futures import ThreadPoolExecutor
from typing import Any, Dict, List, Optional, Tuple


def log(msg: str):
    """Log a message to stderr so it doesn't contaminate JSON on stdout."""
    print(msg, file=sys.stderr, flush=True)

import numpy as np
import pandas as pd
import requests
from datasets import load_dataset
from openai import OpenAI
from tqdm import tqdm

# Constants
ANSWER_PATTERN = re.compile(
    r"(?:answer(?:\sis)?:?\s*)(A|B|C|D|E|F|G|H|I|J)", re.IGNORECASE
)
TIMEOUT_SECONDS = 120
MAX_RETRIES = 2  # Increased for reliability


def parse_args():
    parser = argparse.ArgumentParser(
        description="Evaluate MMLU-Pro benchmark against a vLLM OpenAI endpoint"
    )
    parser.add_argument(
        "--endpoint",
        type=str,
        default=os.getenv("EVAL_ENDPOINT") or "https://api.openai.com/v1",
        help="vLLM OpenAI API endpoint URL",
    )
    parser.add_argument(
        "--model",
        type=str,
        default=os.getenv("EVAL_MODEL"),
        help="Model name to evaluate. If not provided, will be fetched from the API.",
    )
    parser.add_argument(
        "--categories",
        type=str,
        nargs="+",
        default=None,
        help="List of categories to evaluate. If not provided, all available categories will be used.",
    )
    parser.add_argument(
        "--samples-per-category",
        type=int,
        default=5,
        help="Number of questions to sample per category. If not provided, all questions will be used.",
    )
    parser.add_argument(
        "--api-key", type=str, default=os.getenv("OPENAI_API_KEY", "dummy"), help="API key for vLLM endpoint"
    )
    parser.add_argument(
        "--use-cot", action="store_true", help="Use Chain-of-Thought prompting"
    )
    parser.add_argument(
        "--concurrent-requests",
        type=int,
        default=1,
        help="Number of concurrent requests to make",
    )
    parser.add_argument(
        "--max-tokens",
        type=int,
        default=2048,
        help="Maximum number of tokens to generate",
    )
    parser.add_argument(
        "--temperature", type=float, default=0.0, help="Temperature for text generation"
    )
    parser.add_argument(
        "--seed", type=int, default=42, help="Random seed for reproducibility"
    )
    parser.add_argument(
        "--json", action="store_true", default=True, help="Output results in JSON format"
    )
    return parser.parse_args()


def get_available_models(endpoint: str, api_key: str = "dummy") -> List[str]:
    """Get the list of available models from the vLLM OpenAI API endpoint."""
    client = OpenAI(
        base_url=endpoint,
        api_key=api_key,
    )
    try:
        models = client.models.list()
        return [model.id for model in models.data]
    except Exception as e:
        # Try direct HTTP request as fallback
        try:
            response = requests.get(f"{endpoint}/models")
            if response.status_code == 200:
                models_data = response.json()
                return [model["id"] for model in models_data.get("data", [])]
        except Exception:
            pass
        return []


def load_mmlu_pro_dataset(
    categories: Optional[List[str]] = None,
    samples_per_category: Optional[int] = None,
    seed: int = 42,
) -> Tuple[pd.DataFrame, List[str]]:
    """Load the MMLU-Pro dataset and filter by categories if specified."""
    dataset = load_dataset("TIGER-Lab/MMLU-Pro", split="test")
    df = pd.DataFrame(dataset)

    all_categories = sorted(df["category"].unique().tolist())

    if categories:
        df = df[df["category"].isin(categories)]
        if df.empty:
            raise ValueError(
                f"No data found for specified categories. Valid categories are: {', '.join(all_categories)}"
            )

    if samples_per_category:
        random.seed(seed)
        np.random.seed(seed)
        sampled_dfs = []
        for category in df["category"].unique():
            category_df = df[df["category"] == category]
            if len(category_df) > samples_per_category:
                sampled_df = category_df.sample(samples_per_category, random_state=seed)
                sampled_dfs.append(sampled_df)
            else:
                sampled_dfs.append(category_df)
        df = pd.concat(sampled_dfs)

    return df, all_categories


def format_cot_prompt(question: str, options: List[str], use_cot: bool = False) -> str:
    """Format the prompt for the model with or without Chain-of-Thought."""
    letter_mapping = {i: chr(65 + i) for i in range(10)}
    formatted_options = ""

    for i, option in enumerate(options):
        if option.lower() != "n/a":
            formatted_options += f"{letter_mapping[i]}) {option}\n"

    if use_cot:
        prompt = f"Question: {question}\n\nOptions:\n{formatted_options}\n\nPlease solve this step-by-step, then provide your final answer in the format 'Answer: [letter]'."
    else:
        prompt = f"Question: {question}\n\nOptions:\n{formatted_options}\n\nPlease choose the correct answer from the options above. Provide your answer in the format 'Answer: [letter]'."

    return prompt


def extract_answer(response: str) -> Optional[str]:
    """Extract the answer letter from the model's response."""
    match = ANSWER_PATTERN.search(response)
    if match:
        return match.group(1).upper()

    for char in reversed(response):
        if char.upper() in "ABCDEFGHIJ":
            return char.upper()

    return None


def call_model_with_retry(
    client: OpenAI, model: str, prompt: str, max_completion_tokens: int, temperature: float
) -> Tuple[str, int, bool]:
    """Call the model with retry logic for handling timeouts and errors."""
    for attempt in range(MAX_RETRIES):
        # We try different parameter combinations to handle model-specific restrictions
        # (e.g. o1/o3 reasoning models that reject temperature=0 or max_tokens)
        strategies = [
            {"max_completion_tokens": max_completion_tokens, "temperature": temperature},
            {
                "max_completion_tokens": max_completion_tokens
            },  # o1/o3 often prefer no temp or temp=1
            {"max_tokens": max_completion_tokens, "temperature": temperature},
            {"max_tokens": max_completion_tokens},
        ]

        last_error = None
        for strategy in strategies:
            params = {
                "model": model,
                "messages": [{"role": "user", "content": prompt}],
                **strategy,
            }
            try:
                response = client.chat.completions.create(**params)

                if not response.choices or len(response.choices) == 0:
                    log(f"[WARNING] API returned empty choices for model {model}")
                    return "", 0, False

                content = response.choices[0].message.content or ""
                completion_tokens = (
                    response.usage.completion_tokens if response.usage else 0
                )
                return content, completion_tokens, True
            except Exception as e:
                err_msg = str(e).lower()
                last_error = e
                # Fallback on parameter errors
                if any(
                    k in err_msg
                    for k in [
                        "unsupported_parameter",
                        "invalid_request_error",
                        "max_completion_tokens",
                        "max_tokens",
                        "temperature",
                    ]
                ):
                    continue
                else:
                    # Non-parameter error (timeout, auth), try next strategy anyway in this attempt
                    continue

        log(
            f"[ERROR] API call failed (attempt {attempt+1}/{MAX_RETRIES}) for model {model}: {last_error}"
        )
        if attempt < MAX_RETRIES - 1:
            time.sleep(2**attempt)

    return "ERROR", 0, False


def process_question(
    client: OpenAI,
    model: str,
    question_data: Dict[str, Any],
    use_cot: bool,
    max_completion_tokens: int,
    temperature: float,
) -> Dict[str, Any]:
    """Process a single question and return the results."""
    question = question_data["question"]
    options = question_data["options"]
    correct_answer = question_data["answer"]

    prompt = format_cot_prompt(question, options, use_cot)

    start_time = time.time()
    response_text, completion_tokens, success = call_model_with_retry(
        client, model, prompt, max_completion_tokens, temperature
    )
    end_time = time.time()
    duration = end_time - start_time

    predicted_answer = extract_answer(response_text) if success else None
    is_correct = (predicted_answer == correct_answer) if predicted_answer else False

    return {
        "is_correct": is_correct,
        "category": question_data["category"],
        "response_time": duration,
        "completion_tokens": completion_tokens,
        "success": success,
    }


def evaluate_model(
    df: pd.DataFrame,
    model: str,
    endpoint: str,
    api_key: str,
    use_cot: bool,
    concurrent_requests: int,
    max_completion_tokens: int,
    temperature: float,
) -> pd.DataFrame:
    """Evaluate a model on the MMLU-Pro dataset."""
    client = OpenAI(base_url=endpoint, api_key=api_key)
    results = []
    questions_data = df.to_dict("records")

    with ThreadPoolExecutor(max_workers=concurrent_requests) as executor:
        futures = []
        for question_data in questions_data:
            future = executor.submit(
                process_question,
                client,
                model,
                question_data,
                use_cot,
                max_completion_tokens,
                temperature,
            )
            futures.append(future)

        for future in tqdm(futures, total=len(futures), desc=f"Evaluating {model}", file=sys.stderr):
            result = future.result()
            log(f"[INFO] Result for {model}: correct={result.get('is_correct')}, category={result.get('category')}")
            results.append(result)

    return pd.DataFrame(results)


def analyze_results(results_df: pd.DataFrame, model: str) -> Dict[str, Any]:
    """Analyze the results and compute statistics."""
    valid_results = results_df[results_df["success"]]
    if valid_results.empty:
        return {
            "model": model,
            "overall_accuracy": 0.0,
            "avg_response_time": 0.0,
            "tok/s": 0.0,
            "category_accuracy": {}
        }

    overall_accuracy = valid_results["is_correct"].mean()
    avg_response_time = valid_results["response_time"].mean()
    
    # tok/s per request then averaged
    valid_results["tok_per_sec"] = valid_results["completion_tokens"] / valid_results["response_time"]
    avg_tok_per_sec = valid_results["tok_per_sec"].mean()

    category_accuracy = {}
    for category in valid_results["category"].unique():
        category_df = valid_results[valid_results["category"] == category]
        category_accuracy[category] = float(category_df["is_correct"].mean())

    return {
        "model": model,
        "overall_accuracy": float(overall_accuracy),
        "avg_response_time": float(avg_response_time),
        "tok/s": float(avg_tok_per_sec),
        "category_accuracy": category_accuracy
    }


def main():
    args = parse_args()
    random.seed(args.seed)
    np.random.seed(args.seed)

    if not args.model:
        log("No model specified, fetching from API...")
        models = get_available_models(args.endpoint, args.api_key)
        if not models:
            log("[ERROR] Could not retrieve models from endpoint. Exiting.")
            return
        args.model = models[0]

    log(f"Endpoint: {args.endpoint}")
    log(f"Model: {args.model}")
    log(f"API Key: {'***' if args.api_key else '(none)'}")

    df, _ = load_mmlu_pro_dataset(
        categories=args.categories,
        samples_per_category=args.samples_per_category,
        seed=args.seed,
    )
    log(f"Questions to evaluate: {len(df)}")

    results_df = evaluate_model(
        df=df,
        model=args.model,
        endpoint=args.endpoint,
        api_key=args.api_key,
        use_cot=args.use_cot,
        concurrent_requests=args.concurrent_requests,
        max_completion_tokens=args.max_tokens,
        temperature=args.temperature,
    )

    failed = len(results_df[~results_df["success"]])
    succeeded = len(results_df[results_df["success"]])
    log(f"Results: {succeeded} succeeded, {failed} failed out of {len(results_df)} total")

    analysis = analyze_results(results_df, args.model)
    print(json.dumps(analysis, indent=2))


if __name__ == "__main__":
    main()
