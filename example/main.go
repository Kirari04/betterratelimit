package main

import (
	"log"
	"net/http"

	"github.com/Kirari04/betterratelimit"
	"github.com/labstack/echo/v4"
)

func main() {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(betterratelimit.BetterRatelimitGlobal(betterratelimit.DefaultBetterRatelimitGlobalConfig))
	e.GET("/", func(c echo.Context) error {
		return c.String(http.StatusOK, "Page /")
	})
	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusOK, "Page /test")
	})
	e.GET("/stats", func(c echo.Context) error {
		data := betterratelimit.BetterRatelimitGetHistory()
		return c.JSON(http.StatusOK, data)
	})
	log.Println("Running on http://127.0.0.1:1323")
	e.Logger.Fatal(e.Start(":1323"))
}
