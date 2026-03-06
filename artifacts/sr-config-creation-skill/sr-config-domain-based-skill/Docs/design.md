
# Semantic Router Config Generation - Design

## Overview

This system automatically generates a Semantic Router `config.yaml` using
benchmark results from available LLM models. The goal is to route requests
to the best-performing model for each domain.

The system is designed for use with **kagent skills** and **MCP tools**.

## Architecture

Agent workflow:

1. Retrieve model benchmark data
2. Generate routing configuration
3. Deploy configuration to Kubernetes

Pipeline:

Agent
↓
MCP: get_model_benchmarks
↓
Skill: semantic-router-config-skill
↓
generate-router-config.py
↓
config.yaml + k8s manifest
↓
MCP: kubernetes.apply
↓
Semantic Router updated

## Responsibilities

### MCP Tool: get_model_benchmarks

Returns benchmark results for available models.

Example:

[
  {
    "model": "gpt-3.5-turbo",
    "results": {
      "avgResponseTime": 0.6834,
      "categoryAccuracy": {
        "biology": 0.6000,
        "business": 0.4000,
        "chemistry": 0.4000,
        "computer science": 0.6000,
        "economics": 0.2000,
        "engineering": 0.6000,
        "health": 1.0000,
        "history": 0.4000,
        "law": 0.4000,
        "math": 0.2000,
        "other": 0.2000,
        "philosophy": 0.0,
        "physics": 0.2000,
        "psychology": 0.6000
      },
      "overallAccuracy": 0.4143,
      "tokensPerSecond": 20.59
    }
  },
  {
    "model": "gpt-4.1",
    "results": {
      "avgResponseTime": 5.0142,
      "categoryAccuracy": {
        "biology": 1.0000,
        "business": 0.6000,
        "chemistry": 0.6000,
        "computer science": 0.4000,
        "economics": 0.8000,
        "engineering": 0.4000,
        "health": 0.6000,
        "history": 1.0000,
        "law": 0.8000,
        "math": 0.6000,
        "other": 0.6000,
        "philosophy": 0.8000,
        "physics": 0.6000,
        "psychology": 0.6000
      },
      "overallAccuracy": 0.6714,
      "tokensPerSecond": 44.05
    }
  },
  {
    "model": "gpt-oss-120b",
    "results": {
      "avgResponseTime": 2.3899,
      "categoryAccuracy": {
        "biology": 1.0000,
        "business": 0.6000,
        "chemistry": 1.0000,
        "computer science": 0.4000,
        "economics": 1.0000,
        "engineering": 0.6000,
        "health": 0.6000,
        "history": 0.8000,
        "law": 0.4000,
        "math": 0.8000,
        "other": 0.8000,
        "philosophy": 0.4000,
        "physics": 0.6000,
        "psychology": 0.4000
      },
      "overallAccuracy": 0.6714,
      "tokensPerSecond": 198.82
    }
  }
]

### Skill: semantic-router-config-skill

Responsible for:

- invoking the config generation script
- passing benchmark results
- applying the generated manifest

## Routing Optimization

Primary metric:

categoryAccuracy

Tie breaking:

1. lowest avgResponseTime
2. highest tokensPerSecond
3. alphabetical model name

## Output

The script generates:

- config.yaml
The Kubernetes manifest is applied to update the router configuration.
