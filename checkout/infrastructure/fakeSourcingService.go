package infrastructure

import (
	"context"

	cartDomain "flamingo.me/flamingo-commerce/v3/cart/domain/cart"
	"flamingo.me/flamingo-commerce/v3/checkout/domain"
	"github.com/gorilla/sessions"
)

type (
	// FakeDeliveryLocationsService represents the fake source locator
	FakeSourcingService struct {
	}
)

var (
	_ domain.SourcingService = new(FakeSourcingService)
)

// GetDeliveryLocations provides fake delivery locations
func (sl *FakeSourcingService) GetSourceId(ctx context.Context, session *sessions.Session, decoratedCart *cartDomain.DecoratedCart, deliveryCode string, item *cartDomain.DecoratedCartItem) (string, error) {
	return "mock_ispu_location1", nil
}
