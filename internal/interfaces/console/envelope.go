package console

import "github.com/gin-gonic/gin"

type envelope struct {
	Code    int    `json:"code"`
	Data    any    `json:"data"`
	Message string `json:"message"`
}

func Ok(c *gin.Context, data any) { c.JSON(200, envelope{Data: data}) }
func Fail(c *gin.Context, status int, data any, message string) {
	c.AbortWithStatusJSON(status, envelope{Code: status, Data: data, Message: message})
}
