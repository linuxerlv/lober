package media

import (
	"context"
	"github.com/lober/lober/internal/application/media/port"
	mediarules "github.com/lober/lober/internal/domain/media"
)

func identifyNetboot(ctx context.Context, tree port.MediaView) (Identified, error) {
	text, err := identifyText(ctx, tree, ".disk/info")
	if err != nil {
		return Identified{}, err
	}
	return mediarules.Netboot(text)
}
