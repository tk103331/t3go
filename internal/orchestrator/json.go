package orchestrator

import "encoding/json"

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func jsonUnmarshal(data string, v any) error { return json.Unmarshal([]byte(data), v) }
