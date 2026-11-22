package cluster

import (
	"encoding/base64"
	"encoding/json"

	"rollproof/internal/config"
)

// credentialsName is the name of the kubelet's credentials file in the run's folder.
const credentialsName = "node-credentials.json"

// kubeletCredentials is where the kubelet of every node reads its credentials file (section 9).
const kubeletCredentials = "/var/lib/kubelet/config.json"

// Credentials returns the kubelet's credentials file for the nodes, by decision 9: for
// each entry with a username, its host as written and the base64 of
// USERNAME:PASSWORD. Entries without credentials are left out; nil when no
// entry has any.
func Credentials(regs []config.Registry) []byte {
	type auth struct {
		Auth string `json:"auth"`
	}
	auths := map[string]auth{}
	for _, r := range regs {
		if r.Username != "" {
			auths[r.Host] = auth{Auth: base64.StdEncoding.EncodeToString([]byte(r.Username + ":" + r.Password))}
		}
	}
	if len(auths) == 0 {
		return nil
	}
	// MarshalIndent fails only for a value that JSON cannot hold, and a map of strings to a
	// struct of one string is not one.
	b, _ := json.MarshalIndent(map[string]map[string]auth{"auths": auths}, "", "  ")
	return append(b, '\n')
}
