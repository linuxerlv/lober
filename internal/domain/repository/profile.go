package repository

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/lober/lober/internal/domain/model"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/mail"
	"strings"
)

// Avatars are small, embedded raster images: no external requests or active SVG.
func CheckProfile(name, email, avatar, bio string) error {
	invalid := func() error { return fmt.Errorf("%w: invalid profile", ErrInvalid) }
	if len(name) > 200 || len(email) > 254 || len(bio) > 2000 {
		return invalid()
	}
	for _, value := range []string{name, email, bio} {
		if !model.StorableText(value) {
			return invalid()
		}
	}
	if email != "" {
		addr, err := mail.ParseAddress(email)
		if err != nil || addr.Address != email {
			return invalid()
		}
	}
	if avatar == "" {
		return nil
	}
	if len(avatar) > 350000 {
		return invalid()
	}
	var format, data string
	if strings.HasPrefix(avatar, "data:image/png;base64,") {
		format = "png"
		data = strings.TrimPrefix(avatar, "data:image/png;base64,")
	} else if strings.HasPrefix(avatar, "data:image/jpeg;base64,") {
		format = "jpeg"
		data = strings.TrimPrefix(avatar, "data:image/jpeg;base64,")
	} else {
		return invalid()
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil || len(raw) > 256*1024 {
		return invalid()
	}
	config, decoded, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || decoded != format || config.Width < 1 || config.Height < 1 || config.Width > 1024 || config.Height > 1024 {
		return invalid()
	}
	return nil
}
