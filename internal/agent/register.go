package agent

import "github.com/gin-gonic/gin"

func RegisterFromEnv(r *gin.Engine, trustedProxies ...string) error {
	cfg, err := ConfigFromEnv()
	if err != nil {
		return err
	}
	cfg.TrustedProxies = trustedProxies
	model, err := NewDeepSeek(cfg)
	if err != nil {
		return err
	}
	svc, err := NewService(cfg, model)
	if err != nil {
		return err
	}
	svc.Register(r)
	return nil
}
