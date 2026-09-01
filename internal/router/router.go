package router

import (
	"os"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"gorush/internal/handler"
	"gorush/internal/middleware"
	"gorush/internal/redisx"
	"gorush/internal/repository"
	"gorush/internal/service"
)

// New 构造路由，注入依赖（DB、Redis）。
func New(db *gorm.DB, redisStore *redisx.Store) *gin.Engine {
	// 不用 gin.Default()，避免它自带的两个中间件（格式不可控、没有 request_id）。
	r := gin.New()
	r.Use(middleware.RequestID()) // 必须最先：给后续中间件 / handler 提供 req_id
	r.Use(middleware.AccessLog()) // 打印访问日志
	r.Use(middleware.Recovery())  // panic 恢复

	// 失败注入模式（仅 Step 8 教学用；生产应保持空字符串）。
	failMode := os.Getenv("SECKILL_FAIL_AFTER_UPDATE")
	if failMode != "" {
		failMode = "after_update"
	}

	// ---------- /health ----------
	r.GET("/health", handler.NewHealthHandler(db, redisStore).Handle)

	// ---------- 装配 Repository / Service / Handler ----------
	shopTypeRepo := repository.NewShopTypeRepository(db)
	shopTypeSvc := service.NewShopTypeService(shopTypeRepo)
	shopTypeH := handler.NewShopTypeHandler(shopTypeSvc)

	shopRepo := repository.NewShopRepository(db)
	shopSvc := service.NewShopService(shopRepo, redisStore)
	shopH := handler.NewShopHandler(shopSvc)

	voucherRepo := repository.NewVoucherRepository(db)
	voucherSvc := service.NewVoucherService(voucherRepo, shopSvc, redisStore)
	voucherH := handler.NewVoucherHandler(voucherSvc)

	reviewRepo := repository.NewReviewRepository(db)
	reviewSvc := service.NewReviewService(reviewRepo, shopRepo)
	reviewH := handler.NewReviewHandler(reviewSvc)

	favoriteRepo := repository.NewFavoriteRepository(db)
	favoriteSvc := service.NewFavoriteService(favoriteRepo, shopRepo)
	favoriteH := handler.NewFavoriteHandler(favoriteSvc)

	seckillRepo := repository.NewSeckillRepository(db, failMode)
	orderRepo := repository.NewOrderRepository(db)
	orderSvc := service.NewOrderService(seckillRepo, orderRepo)
	orderH := handler.NewOrderHandler(orderSvc)

	// ---------- /api/v1 公开 ----------
	v1 := r.Group("/api/v1")
	{
		v1.GET("/shop-types", shopTypeH.List)
		v1.GET("/shops", shopH.List)
		v1.GET("/shops/nearby", shopH.Nearby)
		v1.GET("/shops/hot", shopH.Hot)
		v1.GET("/shops/:id", shopH.GetByID)
		v1.GET("/shops/:id/vouchers", voucherH.ListByShop)
		v1.GET("/shops/:id/reviews", reviewH.ListByShop)
		v1.POST("/seckill-vouchers", voucherH.CreateSeckill)
	}

	// ---------- /api/v1 登录态 ----------
	authed := r.Group("/api/v1").Use(middleware.Auth())
	{
		authed.POST("/shops/:id/reviews", reviewH.Create)
		authed.POST("/shops/:id/favorite", favoriteH.Add)
		authed.DELETE("/shops/:id/favorite", favoriteH.Remove)
		authed.GET("/users/me/favorites", favoriteH.ListMine)

		authed.POST("/seckill/:voucher_id", orderH.Seckill)
		authed.GET("/orders/:id", orderH.GetByID)
	}

	return r
}
