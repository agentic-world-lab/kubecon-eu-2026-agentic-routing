# How to Query ESIOS Indicator 1001 and Extract the Current Hour Price

This guide explains:

1.  How to generate the API request for the current hour
2.  How to authenticate properly
3.  How to extract the `value` (price €/MWh) field from the response

------------------------------------------------------------------------

## 1️⃣ Endpoint

    GET https://api.esios.ree.es/indicators/1001

Indicator `1001` corresponds to:

**2.0TD tariff PVPC (active energy invoicing price)**

------------------------------------------------------------------------

## 2️⃣ Required Headers

You must include:

-   `Accept: application/json; application/vnd.esios-api-v1+json`
-   `Content-Type: application/json`
-   `X-API-KEY: <YOUR_PERSONAL_TOKEN>`

------------------------------------------------------------------------

## 3️⃣ Build the Request for the Current Hour

If the current time is:

    2026-02-25 15:23:00 (Europe/Madrid)

You must query:

    start_date = 2026-02-25T15:00:00+01:00
    end_date   = 2026-02-25T15:59:59+01:00

------------------------------------------------------------------------

## 4️⃣ Python Example (Generate Current Hour)

``` python
from datetime import datetime
import pytz

tz = pytz.timezone("Europe/Madrid")
now = datetime.now(tz)

start = now.replace(minute=0, second=0, microsecond=0)
end = now.replace(minute=59, second=59, microsecond=0)

start_str = start.isoformat()
end_str = end.isoformat()

print(start_str)
print(end_str)
```

------------------------------------------------------------------------

## 5️⃣ Example Curl Request

``` bash
curl "https://api.esios.ree.es/indicators/1001?start_date=2026-02-25T15:00:00+01:00&end_date=2026-02-25T15:59:59+01:00&geo_ids[]=8741&locale=en"   -H "Accept: application/json; application/vnd.esios-api-v1+json"   -H "Content-Type: application/json"   -H "X-API-KEY: YOUR_REAL_TOKEN"
```

------------------------------------------------------------------------

## 6️⃣ Example Response

``` json
{
  "indicator": {
    "id": 1001,
    "values": [
      {
        "value": 81.86,
        "datetime": "2026-02-25T15:00:00.000+01:00",
        "geo_id": 8741
      }
    ]
  }
}
```

------------------------------------------------------------------------

## 7️⃣ Extract the Price Value

The price is located at:

    indicator.values[0].value

### Python Example

``` python
import requests
from datetime import datetime
from zoneinfo import ZoneInfo


def get_pvpc_price(current_datetime: datetime, api_key: str) -> float:
    """
    Returns the PVPC price (€/MWh) for the hour corresponding
    to the provided datetime in Europe/Madrid timezone.
    
    :param current_datetime: datetime object (naive or timezone-aware)
    :param api_key: Your ESIOS API key
    :return: price in €/MWh (float)
    """

    # Ensure timezone is Europe/Madrid
    madrid_tz = ZoneInfo("Europe/Madrid")

    if current_datetime.tzinfo is None:
        current_datetime = current_datetime.replace(tzinfo=madrid_tz)
    else:
        current_datetime = current_datetime.astimezone(madrid_tz)

    # Round to hour boundaries
    start = current_datetime.replace(minute=0, second=0, microsecond=0)
    end = current_datetime.replace(minute=59, second=59, microsecond=0)

    url = "https://api.esios.ree.es/indicators/1001"

    params = {
        "start_date": start.isoformat(),
        "end_date": end.isoformat(),
        "geo_ids[]": 8741,
        "locale": "en"
    }

    headers = {
        "Accept": "application/json; application/vnd.esios-api-v1+json",
        "Content-Type": "application/json",
        "X-API-KEY": api_key
    }

    response = requests.get(url, headers=headers, params=params)

    if response.status_code != 200:
        raise Exception(f"ESIOS API error: {response.status_code} - {response.text}")

    data = response.json()

    values = data.get("indicator", {}).get("values", [])
    if not values:
        raise ValueError("No price data returned for the specified hour")

    return float(values[0]["value"])

```

------------------------------------------------------------------------

## 8️⃣ Convert €/MWh to €/kWh

    €/kWh = €/MWh ÷ 1000

Example:

    81.86 €/MWh → 0.08186 €/kWh

------------------------------------------------------------------------

## ✅ Summary

To get the current electricity price:

1.  Detect current time in `Europe/Madrid`
2.  Round to the current hour
3.  Query indicator `1001`
4.  Extract:
    data["indicator"]["values"][0]["value"]

That value is the official PVPC price for the current hour in €/MWh.
