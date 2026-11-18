package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type request struct {
	key    string
	id     any
	method string
}

type requests struct {
	calls         []request
	ids           map[string]bool
	notifications int
}

type response struct {
	id    any
	count int
	kind  string
	value any
}

func envelope(item any) (map[string]any, error) {
	obj, ok := item.(map[string]any)
	if !ok {
		return nil, errors.New("expected a JSON-RPC object")
	}
	if obj["jsonrpc"] != "2.0" {
		return nil, errors.New(`expected "jsonrpc": "2.0"`)
	}
	return obj, nil
}

func idKey(id any) (string, error) {
	switch id := id.(type) {
	case string:
		return "s" + id, nil
	case json.Number:
		return "n" + numberKey(id), nil
	case nil:
		return "null", nil
	default:
		return "", errors.New("id must be a number, string, or null")
	}
}

func loadRequests(path string) (requests, error) {
	req := requests{ids: make(map[string]bool)}
	batch, err := readBatch(path, false)
	if err != nil {
		return req, err
	}
	for i, item := range batch {
		obj, err := envelope(item)
		if err != nil {
			return req, fmt.Errorf("request[%d]: %w", i, err)
		}
		method, ok := obj["method"].(string)
		if !ok {
			return req, fmt.Errorf("request[%d]: method must be a string", i)
		}
		if params, ok := obj["params"]; ok {
			switch params.(type) {
			case []any, map[string]any:
			default:
				return req, fmt.Errorf("request[%d]: params must be an array or object", i)
			}
		}
		id, exists := obj["id"]
		if !exists {
			req.notifications++
			continue
		}
		key, err := idKey(id)
		if err != nil {
			return req, fmt.Errorf("request[%d]: %w", i, err)
		}
		if req.ids[key] {
			return req, fmt.Errorf("request[%d]: duplicate request id", i)
		}
		req.calls = append(req.calls, request{key: key, id: id, method: method})
		req.ids[key] = true
	}
	return req, nil
}

func loadResponses(path string) (map[string]response, error) {
	batch, err := readBatch(path, true)
	if err != nil {
		return nil, err
	}
	responses := make(map[string]response)
	for i, item := range batch {
		obj, err := envelope(item)
		if err != nil {
			return nil, fmt.Errorf("response[%d]: %w", i, err)
		}
		id, hasID := obj["id"]
		if !hasID {
			return nil, fmt.Errorf("response[%d]: missing id", i)
		}
		key, err := idKey(id)
		if err != nil {
			return nil, fmt.Errorf("response[%d]: %w", i, err)
		}
		result, hasResult := obj["result"]
		rpcError, hasError := obj["error"]
		if hasResult == hasError {
			return nil, fmt.Errorf("response[%d]: expected exactly one of result or error", i)
		}
		kind, value := "result", result
		if hasError {
			if err := validateError(rpcError); err != nil {
				return nil, fmt.Errorf("response[%d]: %w", i, err)
			}
			kind, value = "error", rpcError
		}
		responses[key] = response{id: id, count: responses[key].count + 1, kind: kind, value: value}
	}
	return responses, nil
}

func validateError(value any) error {
	obj, ok := value.(map[string]any)
	if !ok {
		return errors.New("error must be an object")
	}
	code, ok := obj["code"].(json.Number)
	if !ok || strings.Contains(numberKey(code), "e-") {
		return errors.New("error.code must be an integer")
	}
	if _, ok := obj["message"].(string); !ok {
		return errors.New("error.message must be a string")
	}
	return nil
}
