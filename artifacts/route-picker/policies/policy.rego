package routepicker

# Policy: compute the header value to add. The server evaluates
# `data.routepicker.header_value` and expects a string.

# Default value when no override header is present or policy does not match.
default header_value = "cpu"

# If the request contains a header `x-override-route-picker` equal to
# "cpu" then return "cpu" as the chosen route.
header_value = "cpu" {
    h := input.headers["x-example"]
    h == "cpu"
   
}

# si el min es par devuelve cpu x-route-picker=cpu
# You can extend this rule to accept other override values or to return the
# header value directly (e.g., `header_value = h { h := input.headers["x-override-route-picker"] }`).
