// Package fpnative is the only File Provider package that imports native APIs.
package fpnative

import "encoding/json"

type Result struct {
	Supported    bool     `json:"supported"`
	GroupPath    string   `json:"groupPath"`
	Secret       string   `json:"secret"`
	Domains      []string `json:"domains"`
	RecoveryPath string   `json:"recoveryPath"`
	Error        string   `json:"error"`
}

func Call(operation, domain string) (Result, error) {
	b, _ := json.Marshal(map[string]string{"operation": operation, "domain": domain})
	return call(b)
}
