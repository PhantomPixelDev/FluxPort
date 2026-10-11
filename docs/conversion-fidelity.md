# Conversion fidelity

FluxPort 0.2.1 hardens conversions around preserving values and reporting format limitations. It does not claim that every format can represent every structure.

## Fixed cases

- Nested YAML with multiline values and leading spaces now uses a two-space encoder. This avoids invalid block-scalar indentation. Buffered and streamed YAML use the same serializer.
- YAML numbers are decoded without float64 coercion. Large integers, long decimals and very small exponents retain their numeric type and precise lexeme. Date/time scalars retain their original text.
- JSON duplicate keys, invalid UTF-8 and unpaired Unicode surrogates are rejected instead of silently overwriting/replacing values. Errors identify the duplicate key and its object path.
- TOML imports and exports reject numeric values that would overflow, underflow or lose decimal precision. Integers must fit signed 64 bits; null and array roots remain unsupported. Error messages identify the field path or source position.
- XML mixed text/element content uses an ordered `#content` list. Leaf text retains surrounding spaces. Prefixes remain distinct even when two prefixes refer to the same namespace URI. Malformed nesting and duplicate attributes are rejected.
- CSV exports reject unknown schema fields instead of silently dropping them. Header-only CSV-to-CSV conversions retain their columns. Spreadsheet-safe encoding preserves embedded newline bytes and sanitizes formula-like headers as well as string cells.
- Converter results include relevant format notes. Changing input/options or failing a conversion marks the previous output clearly. Empty NDJSON is a valid downloadable result. Native form submissions retain pasted input and format selections, including on parsing failure.

## CSV controls

The converter defaults to automatic input delimiter detection across comma, semicolon, tab and pipe. Ambiguous delimiters require an explicit selection. Output CSV uses commas.

Type inference is optional. It recognizes JSON-style numbers, case-insensitive true/false and null. It preserves leading-zero IDs (`00123`), empty cells, whitespace, plus-prefixed strings, dates and non-numeric text. Large inferred numbers use exact numeric lexemes. It does not parse nested JSON-looking cells automatically.

Spreadsheet-safe CSV is enabled in the converter and Prepare download UI. Formula-like text and headers receive an apostrophe prefix; numeric values remain numeric text. This intentionally changes those text cells, so disable the option when exact text preservation is required. Source-file downloads and machine/API format responses retain their existing semantics.

Collection discovery retains comma-delimited, text-cell CSV behavior for compatibility. CLI conversion retains those defaults unless flags are supplied:

```text
fluxport convert input.csv --to json --delimiter auto --infer-types --output output.json
fluxport convert input.json --to csv --spreadsheet-safe --output output.csv
```

The same options are used in buffered conversion, streaming conversion and persisted background jobs, including retries.

## Representation differences

YAML block markers such as `|2` preserve multiline strings and their leading spaces. An explicit `!!int` tag can appear for integers beyond the usual machine range; it preserves the numeric type. These markers are format syntax, not extra data.

CSV is flat text. Nested objects/arrays become JSON text in cells; null and missing fields become empty cells. Quoting and escaping are required CSV syntax. CSV readers normalize CRLF within quoted fields to LF, following the Go CSV reader's behavior.

XML export uses a FluxPort JSON envelope to retain arrays, numeric types and nulls. Entities such as `&lt;` and `&amp;` are required XML escaping. General XML import is a data mapping rather than preservation of its complete source document: attributes use @keys, element-only children are grouped by name, mixed content uses #content, and comments/processing instructions are ignored. Whitespace between element-only children is ignored as layout.

NDJSON is a record sequence. An object or scalar becomes one record; an array root becomes multiple records. Converting that sequence back to JSON yields an array. Empty NDJSON represents an empty sequence; empty TOML represents an empty object. Both can be downloaded and imported again.

TOML dates/times become ISO strings. Date-only values remain dates; timezone spelling may be normalized. YAML custom tags, non-string object keys and non-finite numbers cannot be represented in JSON and are rejected. YAML merges and aliases are supported with bounded expansion. JSON/YAML nesting is limited to 512 levels; general XML retains its 64-level limit.

## Regression coverage

Tests cover multi-level objects/arrays, empty containers, nulls, booleans, long and oversized numbers, underflow, dates, Unicode, escaping, duplicate keys, namespaces, mixed XML text, YAML merges/aliases, multiline strings with leading spaces, CSV headers/delimiters/inference, formula-like values, and source-order streaming. Web and CLI tests verify options across small and streamed input; job tests verify persisted conversion options.

The Playwright callback in scripts/converter-checks.js exercises converter input/upload, nested round trips, delimiter/inference controls, spreadsheet-safe CSV, previous-result labels, error clearing, header-only CSV, an empty NDJSON download, and desktop/mobile layout. It can be passed to Playwright CLI run-code after removing comment-only lines and joining remaining lines with spaces.
