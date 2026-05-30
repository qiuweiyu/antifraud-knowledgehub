# Risk Engine

The risk engine is explainable by design. Every match returns evidence, rule metadata, scoring weight, explanation and a recommended action.

## Rule Types

- `keyword`: comma-separated literal terms.
- `regex`: regular expressions for URLs, amounts, time pressure and contact patterns.
- `pattern`: composite phrases that represent common scam flows.
- `semantic_placeholder`: mock provider hook reserved for future AI-assisted analysis.

## Scoring

Each matched rule contributes its configured `weight`. The total score is capped at 100.

- `low`: 0-29
- `medium`: 30-59
- `high`: 60-79
- `critical`: 80-100

## Output

Matched rules include `rule_code`, `rule_name`, `category_code`, `weight`, `severity`, `evidence`, `explanation` and `recommendation`.

## Future Provider Interface

`semantic_placeholder` rules are implemented as local mock matching in v0.1. They reserve the contract for future optional model or embedding adapters without making local development depend on paid external APIs.
