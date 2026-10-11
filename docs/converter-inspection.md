# Converter inspection

The converter now includes a conversion report with input record/byte counts, output size, target-format notes, and sampled CSV cell-change counts. CSV counts inspect the first 1,000 records and say when sampled.

Enable **Validate round trip** to read the output back and compare normalized values and types. Differences identify field paths, including missing fields and changed array lengths. Source formatting is not compared. Comparison stops at 10,000 visited values or 50 differences and explicitly reports that limit. Large background conversions keep streaming and do not build document reports or run this comparison.

**Explore input/output structure** opens keyboard-accessible, collapsible trees. Numeric text remains exact. Trees show up to 500 nodes and 10 levels; long scalar previews are truncated to 500 bytes. Raw input and downloadable output remain available. Uploaded input previews read only the first 256 KiB; the full file is converted, and truncated previews are labelled.

CSV column rules use one exact header name=type per line:

```text
id=text
price=number
active=boolean
metadata=json
```

Supported types are text, number, boolean, null, json and auto. Explicit text rules protect identifiers and override inference/nested JSON parsing. Unknown header names and invalid typed values produce errors. Number uses JSON number syntax; boolean accepts true/false; null accepts empty/null. Optional nested JSON decoding handles object/array cells and reports invalid JSON. Rules are shared by buffered, streaming and persisted-job conversions.

CLI equivalents: `--column-types` accepts the same multiline rules; `--parse-json-cells` enables nested cells. Existing conversion defaults remain compatible.

JSON/CSV/XML parse errors include available line/column information and source excerpts. Other parser diagnostics retain their native positions and field paths, with a format-specific correction hint.

Regression coverage includes 300 deterministically generated nested values through JSON/YAML/XML, TOML and NDJSON cases, CSV rules/streaming parity, bounded comparisons, exact preview numbers and web error/report flows. `FuzzConversionRoundTrip` also exercises arbitrary JSON inputs. Browser interactions are reproducible with `scripts/converter-inspection-checks.js` using the same Playwright callback method as the existing converter checks.
