"""Pricing tool for OpenRouter MCP server.
Provides token pricing from OpenRouter.
"""

import os
import requests
import json
from core.server import mcp


@mcp.tool()
def get_model_pricing(model_name: str) -> str:
    """
    Returns the token pricing (prompt and completion) for a model from OpenRouter.
    
    Args:
        model_name: Name or ID of the model (e.g. 'gpt-4o', 'openai/gpt-4o').

    Returns:
        JSON string in the format: {"pricing": [{"prompt": "...", "completion": "..."}]}
    """
    url = "https://openrouter.ai/api/v1/models"
    
    try:
        response = requests.get(url, timeout=15)
        response.raise_for_status()
        data = response.json()
        models_data = data.get("data", [])
        
        target = model_name.lower()
        best_match = None
        
        # 1st pass: strict matches
        for m in models_data:
            m_id = str(m.get("id", "")).lower()
            m_name = str(m.get("name", "")).lower()
            
            # Exact ID
            if m_id == target:
                best_match = m
                break
            
            # Match after provider (e.g. "openai/gpt-4o" matches "gpt-4o")
            if "/" in m_id and m_id.split("/")[-1] == target:
                best_match = m
                break
                
            # Exact Name
            if m_name == target:
                best_match = m
                break
        
        # 2nd pass: partial matches if no strict match
        if not best_match:
            for m in models_data:
                m_id = str(m.get("id", "")).lower()
                m_name = str(m.get("name", "")).lower()
                
                if target in m_id or target in m_name:
                    best_match = m
                    break
                    
        if best_match:
            pricing = best_match.get("pricing", {})
            prompt_raw = pricing.get("prompt", "0")
            completion_raw = pricing.get("completion", "0")
            
            # Convert to float and multiply by 1 million
            prompt_1m = float(prompt_raw) * 1000000
            completion_1m = float(completion_raw) * 1000000
            
            return json.dumps({
                "pricing": [
                    {
                        "prompt": f"{prompt_1m:.2f}",
                        "completion": f"{completion_1m:.2f}"
                    }
                ]
            })
            
        return json.dumps({"pricing": []})
        
    except requests.exceptions.RequestException as e:
        raise Exception(f"Failed to fetch models from OpenRouter: {e}")
    except Exception as e:
        raise Exception(f"An unexpected error occurred: {str(e)}")
