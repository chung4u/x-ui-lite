package web

import (
 "testing"
 "github.com/gin-gonic/gin"
)

func TestEmbeddedTranslationsLoad(t *testing.T) {
 s := &Server{}
 if err := s.initI18n(gin.New()); err != nil { t.Fatal(err) }
}
