# rpcbatchdiff

Compare two saved JSON-RPC batch responses against the requests that produced them. An Ethereum indexer can use this during a provider migration or a client upgrade. The command reads three local files and matches replies by request ID.

## Try it

Build with Go 1.22 or later. The project uses the Go standard library.

```sh
go build -o rpcbatchdiff .
./rpcbatchdiff examples/requests.json examples/left.json examples/right.json
```

The synthetic fixtures describe calls against block `0x100` on a local chain. The second capture reorders the replies, gives a different priority fee and reports an unavailable receipts method.

```text
calls=3 notifications=0 matched=1 matched_errors=0 findings=2
result_changed id=9007199254740993 method="eth_feeHistory" path="/result/reward/1/0" left="0x3" right="0x4"
outcome_changed id="receipts" method="eth_getBlockReceipts" left={"result":[]} right={"error":{"code":-32601,"message":"Method not found"}}
```

For your own captures, use the same chain and block for both calls. A moving block tag such as `latest` can produce genuine differences between healthy providers.

## Use in CI

```sh
./rpcbatchdiff -json requests.json before.json after.json
```

| Exit | Meaning |
| ---- | ------- |
| 0 | Every expected reply appeared once on each side and the outcomes matched. |
| 1 | The report contains at least one finding. |
| 2 | The command could not read, validate or write the requested data. Details go to stderr. |

The report includes the numbers of calls, notifications, matched pairs and matching error pairs. Each finding includes a typed `id`. The command reports one of the following kinds.

| Kind | Fields and meaning |
| ---- | ------------------ |
| `missing` | The named `side` has no reply for the expected ID. |
| `duplicate` | The named `side` contains `count` replies for an expected ID. |
| `unexpected` | The ID was absent from the request batch. `count` records all its occurrences on that side. |
| `result_changed`, `error_changed` | The first different value for this call, with its JSON Pointer and the values from each side. A missing `left` or `right` field denotes absence. An explicit `null` denotes a JSON value. |
| `outcome_changed` | One side returned a result and the other an error. Each value includes its outcome name. |

Text output shortens long values. JSON output preserves the complete reported values and numeric IDs. Use a precision-preserving JSON reader for these fields.

## Comparison rules

- The request file must contain a nonempty batch with unique IDs. A request without an ID is a notification. An explicit `null` ID is a call. The response file is a batch array or an empty body when no reply arrived. An all-notification batch with two empty files matches. A missing reply for the same call on both sides yields two findings. [JSON-RPC 2.0](https://www.jsonrpc.org/specification)
- String and number IDs are distinct. Numbers compare by exact decimal value, including exponent notation. `1`, `1.0` and `1e0` match. `9007199254740992` and `9007199254740993` differ.
- Object member order is irrelevant. Array order inside a result or error matters. Strings compare by their decoded contents, so Ethereum hex strings keep their spelling and leading zeroes. The tool compares complete error objects, including optional data.
- Changed values are visited in lexicographic object-key order and array-index order. Their paths are relative to the response object. [JSON Pointer](https://www.rfc-editor.org/rfc/rfc6901)
- Each file must hold one UTF-8 JSON document, apart from the empty-response case. Duplicate object member names, malformed envelopes, and singleton responses to batch input produce exit 2. The command holds both captures in memory.

## Development

```sh
go test -race ./...
go vet ./...
go test -fuzz=FuzzDecimalComparison -fuzztime=10s
```
