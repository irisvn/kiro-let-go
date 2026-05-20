package antiban

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

var probeUserAgents = []string{
	"health",
	"probe",
	"elb",
	"kube-probe",
	"prometheus",
	"uptimerobot",
	"pingdom",
	"statuscake",
}

var probePaths = map[string]struct{}{
	"/healthz": {},
	"/readyz":  {},
	"/livez":   {},
}

// IsHealthProbe detects whether an incoming request is an automated
// health or uptime probe.
func IsHealthProbe(r *http.Request) bool {
	ua := strings.ToLower(r.UserAgent())
	for _, probe := range probeUserAgents {
		if strings.Contains(ua, probe) {
			return true
		}
	}

	if _, ok := probePaths[r.URL.Path]; ok {
		return true
	}

	if r.Header.Get("X-Health-Check") != "" {
		return true
	}

	return false
}

// HealthProbeMiddleware returns a gin middleware that short-circuits
// health-probe requests with a 200 OK so they never hit auth, DB, or
// upstream Kiro calls.
func HealthProbeMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsHealthProbe(c.Request) {
			c.Writer.Header().Set("Content-Type", "application/json")
			c.String(http.StatusOK, `{"status":"ok"}`)
			c.Abort()
			return
		}
		c.Next()
	}
}
