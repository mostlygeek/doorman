package main

import (
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"

	"github.com/mozilla/doorman/doorman"
	"github.com/mozilla/doorman/utilities"
)

// DefaultPoliciesFilename is the default policies filename.
const DefaultPoliciesFilename string = "policies.yaml"

func setupRouter() (*gin.Engine, error) {
	r := gin.New()
	// Crash free (turns errors into 5XX).
	r.Use(gin.Recovery())

	// Setup logging.
	r.Use(HTTPLoggerMiddleware())

	// Setup doorman and load configuration files.
	w := doorman.New(sources())
	if err := w.LoadPolicies(); err != nil {
		return nil, err
	}

	doorman.SetupRoutes(r, w)

	utilities.SetupRoutes(r)

	return r, nil
}

func sources() []string {
	// If POLICIES not specified, read ./policies.yaml
	env := os.Getenv("POLICIES")
	if env == "" {
		env = DefaultPoliciesFilename
	}
	sources := strings.Split(env, " ")
	// Filter empty strings
	var r []string
	for _, v := range sources {
		s := strings.TrimSpace(v)
		if s != "" {
			r = append(r, s)
		}
	}
	return r
}

func main() {
	r, err := setupRouter()
	if err != nil {
		log.Fatal(err.Error())
	}
	r.Run() // listen and serve on 0.0.0.0:$PORT (:8080)
}
