package utils

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"k8s.io/apimachinery/pkg/types"
)

func CreateTargetsPath(
	router *gin.Engine,
	nn types.NamespacedName,
	handler gin.HandlerFunc,
) {
	path := fmt.Sprintf(
		"/api/v1/%s/target-source/%s/createTargets",
		nn.Namespace,
		nn.Name,
	)

	router.POST(path, handler)
}
