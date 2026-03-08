# Dynamic LLM Token Pricing Design

## Goal

Implement a pricing adaptation mechanism that takes the public API price
of a model and adjusts it to a local deployment where the only variable
cost known is electricity.

The system removes the estimated electricity portion from the public
price and replaces it with the local electricity cost while keeping the
rest of the infrastructure cost unchanged.

The result preserves: - the input/output token price ratio used by the
provider - the non‑electricity infrastructure costs - dynamic
electricity pricing

All prices are expressed per **1M tokens**.

------------------------------------------------------------------------

# Conceptual Model

Public LLM pricing can be decomposed as:

P_api = P_other + P_electricity

Where:

P_other includes: - GPU amortization - datacenter infrastructure -
networking - engineering/operations

P_electricity represents the electricity consumption component.

Typical estimate:

electricity_share (α) ≈ 0.20

So:

P_electricity = α \* P_avg P_other = (1 - α) \* P_avg

------------------------------------------------------------------------

# Variables

Required inputs:

p_in_api → Public prompt price ($/1M tokens)
p_out_api     → Public completion price ($/1M tokens)\
elec_local → Local electricity price (\$/kWh)

Optional parameters:

elec_public → Assumed provider electricity price (\$/kWh), default =
0.07\
alpha → Electricity share of total cost, default = 0.20

Derived values:

r → Output/Input price ratio\
p_avg → Average token price

------------------------------------------------------------------------

# Algorithm

## 1 Compute output/input ratio

r = p_out_api / p_in_api

This preserves the decoding vs prefill cost asymmetry used by the
provider.

------------------------------------------------------------------------

## 2 Compute average token price

p_avg = (p_in_api + p_out_api) / 2

This avoids assumptions about prompt/completion distribution.

------------------------------------------------------------------------

## 3 Separate electricity component

p_elec_api = p_avg \* alpha p_other = p_avg \* (1 - alpha)

------------------------------------------------------------------------

## 4 Scale electricity to local price

p_elec_local = p_elec_api \* (elec_local / elec_public)

------------------------------------------------------------------------

## 5 Compute new average token price

p_avg_local = p_other + p_elec_local

------------------------------------------------------------------------

## 6 Recover input/output prices

p_in_local = (2 \* p_avg_local) / (1 + r)

p_out_local = r \* p_in_local

This ensures the final average token price equals p_avg_local while
preserving the original ratio.

------------------------------------------------------------------------

# Python Reference Implementation

``` python
def adapt_llm_price(
    p_in_api,
    p_out_api,
    elec_local,
    elec_public=0.07,
    alpha=0.2
):
    # ratio between output and input tokens
    r = p_out_api / p_in_api

    # average token price
    p_avg = (p_in_api + p_out_api) / 2

    # split electricity vs other costs
    p_other = p_avg * (1 - alpha)
    p_elec_api = p_avg * alpha

    # scale electricity with local price
    p_elec_local = p_elec_api * (elec_local / elec_public)

    # new average token price
    p_avg_local = p_other + p_elec_local

    # recompute input/output prices
    p_in_local = (2 * p_avg_local) / (1 + r)
    p_out_local = r * p_in_local

    return p_in_local, p_out_local
```

------------------------------------------------------------------------

# Example

Input:

p_in_api = 0.039\
p_out_api = 0.19\
elec_local = 0.15907

Output (approx):

prompt price ≈ 0.033 \$ / 1M tokens\
completion price ≈ 0.160 \$ / 1M tokens

------------------------------------------------------------------------

# Integration Suggestions

The pricing function can be executed:

• hourly using electricity market prices\
• when updating a model CRD in Kubernetes\
• when refreshing inference cost metrics

Example architecture:

Electricity Market API ↓ Pricing Controller ↓ LLMBackend CRD
status.price ↓ Inference Gateway Billing

------------------------------------------------------------------------

# Future Improvements

Possible enhancements:

• use real GPU power consumption instead of fixed α\
• adapt α per model size\
• compute decode/prefill ratio from benchmark metrics\
• integrate datacenter PUE factor

------------------------------------------------------------------------

End of design document
