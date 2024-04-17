package main

import (
	"context"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"time"

	"github.com/Kirari04/betterratelimit"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func main() {
	t := &Template{
		templates: template.Must(template.ParseGlob("example/views/*.html")),
	}

	e := echo.New()
	e.Renderer = t
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.TimeoutWithConfig(middleware.TimeoutConfig{
		Timeout: 30 * time.Second,
	}))

	// ##################################################################################################
	// # Add the Global Ratelimit handler here                                                          #
	// ##################################################################################################
	e.Use(betterratelimit.BetterRatelimitGlobal(betterratelimit.DefaultBetterRatelimitGlobalConfig))

	e.GET("/", func(c echo.Context) error {
		return c.Render(http.StatusOK, "index.html", echo.Map{})
	})
	e.GET("/test", func(c echo.Context) error {
		return c.Render(http.StatusOK, "test.html", echo.Map{})
	})
	e.GET("/stats", func(c echo.Context) error {
		return c.Render(http.StatusOK, "stats.html", echo.Map{})
	})
	e.GET("/api/stats", func(c echo.Context) error {
		type PathData struct {
			Path     string `json:"name"`
			Requests []uint `json:"data"`
		}
		input := betterratelimit.BetterRatelimitGetHistory()

		// Define the time range for which you want to collect data (e.g., past 6 seconds)
		now := time.Now()
		lastNSeconds := 60

		output := make([]PathData, 0)

		// Find all available paths in input
		availablePaths := make(map[string]bool)
		for _, counts := range input {
			for path := range counts {
				availablePaths[path] = true
			}
		}

		// Populate output with all those paths
		pathIndexMap := make(map[string]int)
		i := 0
		for path := range availablePaths {
			output = append(output, PathData{
				Path:     path,
				Requests: make([]uint, lastNSeconds),
			})
			pathIndexMap[path] = i
			i++
		}

		for ts, pathStats := range input {
			tsDif := now.Unix() - ts.Unix()
			if tsDif < int64(lastNSeconds) {
				for path, reqs := range pathStats {
					dataIndex := (tsDif - int64(lastNSeconds-1)) * -1
					output[pathIndexMap[path]].Requests[dataIndex] = reqs
				}
			}
		}

		// Sort the output by path
		sort.Slice(output, func(i, j int) bool {
			return output[i].Path < output[j].Path
		})

		return c.JSON(http.StatusOK, output)
	})
	e.Logger.Info("Running on http://127.0.0.1:1323")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	// Start server
	go func() {
		if err := e.Start(":1323"); err != nil && err != http.ErrServerClosed {
			e.Logger.Fatal("shutting down the server")
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server with a timeout of 10 seconds.
	<-ctx.Done()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		e.Logger.Fatal(err)
	}
}
