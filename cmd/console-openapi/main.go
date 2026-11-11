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
	if err := json.NewEncoder(os.Stdout).Encode(console.OpenAPIDocument()); err != nil {
		panic(err)
	}
}
