Create an MCP by using the framework FastMCP in python able to get the current price of electricity in Spain following the indications in the file @api_red_electrica_española.md.
Create all the folders using the best practices for MCPs.
You should create a class `ElectricityPriceMCP` that extends `BaseMCP`.
The constructor should accept the following parameters:
- `api_key`: Your ESIOS API key and in case of not being provided, it should use request_your_personal_token_sending_email_to_consultasios@ree.es.
The method `get_price` should return the current price of electricity in Spain.


