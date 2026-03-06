"""Cost tool for MCP server.
"""

import os
import requests
from datetime import datetime
from zoneinfo import ZoneInfo
from core.server import mcp


@mcp.tool()
def get_current_price() -> float:
    """
    Returns the current PVPC electricity price in Spain (€/kWh) for the 2.0TD tariff.
    """
    # Use environment variable for API key if available, otherwise use default
    api_key = os.environ.get("ESIOS_API_KEY", "request_your_personal_token_sending_email_to_consultasios@ree.es")
    
    # Ensure timezone is Europe/Madrid as per API requirements
    madrid_tz = ZoneInfo("Europe/Madrid")
    now = datetime.now(madrid_tz)

    # Round to hour boundaries (start of hour to end of hour)
    start = now.replace(minute=0, second=0, microsecond=0)
    end = now.replace(minute=59, second=59, microsecond=0)

    url = "https://api.esios.ree.es/indicators/1001"

    params = {
        "start_date": start.isoformat(),
        "end_date": end.isoformat(),
        "geo_ids[]": 8741,  # Peninsula
        "locale": "en"
    }

    headers = {
        "Accept": "application/json; application/vnd.esios-api-v1+json",
        "Content-Type": "application/json",
        "X-API-KEY": api_key
    }

    try:
        response = requests.get(url, headers=headers, params=params, timeout=10)
        response.raise_for_status()
        data = response.json()
        
        values = data.get("indicator", {}).get("values", [])
        if not values:
            raise ValueError("No price data returned for the current hour from ESIOS API.")
        
        # The price is in indicator.values[0].value as per documentation
        price_mwh = float(values[0]["value"])
        return price_mwh / 1000.0
        
    except requests.exceptions.RequestException as e:
        raise Exception(f"ESIOS API request failed: {str(e)}")
    except (KeyError, IndexError, ValueError) as e:
        raise Exception(f"Failed to parse ESIOS API response: {str(e)}")
    except Exception as e:
        raise Exception(f"An unexpected error occurred: {str(e)}")
