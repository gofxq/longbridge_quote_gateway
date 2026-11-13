package server

import (
	"net/url"

	"github.com/gin-gonic/gin"
)

func httpQuery(c *gin.Context) (url.Values, error) { return url.ParseQuery(c.Request.URL.RawQuery) }
