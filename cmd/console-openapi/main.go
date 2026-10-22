// console-openapi exports the actual route descriptors without starting a server.
package main

import (
	"encoding/json"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/interfaces/console"
)

func main() {
	gin.SetMode(gin.ReleaseMode)
	s := console.New(console.Deps{Clock: func() int64 { return 0 }})
	if err := json.NewEncoder(os.Stdout).Encode(s.OpenAPI()); err != nil {
		panic(err)
	}
}
