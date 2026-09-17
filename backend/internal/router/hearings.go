package router

import (
	"cylawcase/internal/middleware"

	"github.com/gin-gonic/gin"
)

// registerHearingRoutes 庭审排期路由。
func (r *Router) registerHearingRoutes(g *gin.RouterGroup) {
	hearings := g.Group("/hearings")
	hearings.Use(middleware.AuthRequired(r.cfg))
	hearings.GET("", r.hearing.List)
	hearings.GET("/upcoming", r.hearing.Upcoming)
	hearings.GET("/by-case/:id/history", r.hearing.HistoryByCase)
	hearings.GET("/:id", r.hearing.Get)
	hearings.POST("", r.hearing.Schedule)
	hearings.POST("/:id/reschedule", r.hearing.Reschedule)
	hearings.POST("/:id/cancel", r.hearing.Cancel)
}
