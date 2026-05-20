package antiban

import (
	"hash/fnv"
	"net/http"
	"reflect"
	"strings"

	"github.com/google/uuid"
)

var kiroHeaderVersions = []string{"1.0.31", "1.0.32", "1.0.34", "1.0.33"}
var kiroHeaderOS = []string{"darwin", "linux", "win32"}

// OnceFor returns a deterministic selection index for accountID within listLen.
func OnceFor(accountID string, listLen int) int {
	if listLen <= 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(accountID))
	return int(h.Sum64() % uint64(listLen))
}

// BuildKiroRequestHeaders builds stable per-account Kiro request headers.
func BuildKiroRequestHeaders(acc any, region string) http.Header {
	headers := make(http.Header)
	if acc == nil {
		return headers
	}

	id := stringField(acc, "ID")
	authMethod := stringField(acc, "AuthMethod")
	machineID := strings.TrimSpace(stringField(acc, "MachineID"))
	token := stringPtrField(acc, "AccessToken")
	if token == "" {
		token = stringPtrField(acc, "APIKey")
	}

	version := kiroHeaderVersions[OnceFor(id, len(kiroHeaderVersions))]
	osName := kiroHeaderOS[OnceFor(id+":os", len(kiroHeaderOS))]
	if machineID == "" {
		machineID = "unknown"
	}

	headers.Set("Authorization", "Bearer "+token)
	headers.Set("Content-Type", "application/json")
	headers.Set("Connection", "close")
	headers.Set("host", "q."+region+".amazonaws.com")
	headers.Set("x-amzn-codewhisperer-optout", "true")
	headers.Set("x-amzn-kiro-agent-mode", "vibe")
	headers.Set("amz-sdk-invocation-id", uuid.NewString())
	headers.Set("amz-sdk-request", "attempt=1; max=3")
	if strings.EqualFold(authMethod, "apikey") || strings.EqualFold(authMethod, "api_key") {
		headers.Set("tokentype", "API_KEY")
	}
	headers.Set("User-Agent", "aws-sdk-js/"+version+" ua/2.1 os/"+osName+" lang/js md/nodejs#v20.10.0 api/codewhispererstreaming#"+version+" m/E KiroIDE-"+version+"-"+machineID)
	headers.Set("x-amz-user-agent", "aws-sdk-js/"+version+" KiroIDE-"+version+"-"+machineID)

	return headers
}

func stringField(v any, name string) string {
	rv := reflect.Indirect(reflect.ValueOf(v))
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return ""
	}
	f := rv.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.String {
		return ""
	}
	return f.String()
}

func stringPtrField(v any, name string) string {
	rv := reflect.Indirect(reflect.ValueOf(v))
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return ""
	}
	f := rv.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.Pointer || f.Type().Elem().Kind() != reflect.String || f.IsNil() {
		return ""
	}
	return f.Elem().String()
}
