package checkout

import (
	"github.com/go-playground/form"
	"go.aoe.com/flamingo/core/checkout/domain"
	paymentDomain "go.aoe.com/flamingo/core/checkout/domain/payment"
	"go.aoe.com/flamingo/core/checkout/infrastructure"
	paymentInfrastructure "go.aoe.com/flamingo/core/checkout/infrastructure/payment"
	"go.aoe.com/flamingo/core/checkout/interfaces/controller"
	"go.aoe.com/flamingo/framework/config"
	"go.aoe.com/flamingo/framework/dingo"
	"go.aoe.com/flamingo/framework/router"
)

type (
	// CheckoutModule registers our profiler
	CheckoutModule struct {
		RouterRegistry                  *router.Registry `inject:""`
		UseFakeDeliveryLocationsService bool             `inject:"config:checkout.useFakeDeliveryLocationsService,optional"`
	}
)

// Configure module
func (m *CheckoutModule) Configure(injector *dingo.Injector) {

	m.RouterRegistry.Handle("checkout.start", (*controller.CheckoutController).StartAction)
	m.RouterRegistry.Route("/checkout", "checkout.start")

	m.RouterRegistry.Handle("checkout.review", (*controller.CheckoutController).ReviewAction)
	m.RouterRegistry.Route("/checkout/review", `checkout.review`)

	m.RouterRegistry.Handle("checkout.guest", (*controller.CheckoutController).SubmitGuestCheckoutAction)
	m.RouterRegistry.Route("/checkout/guest", "checkout.guest")

	m.RouterRegistry.Handle("checkout.user", (*controller.CheckoutController).SubmitUserCheckoutAction)
	m.RouterRegistry.Route("/checkout/user", "checkout.user")

	m.RouterRegistry.Handle("checkout.success", (*controller.CheckoutController).SuccessAction)
	m.RouterRegistry.Route("/checkout/success", "checkout.success")

	m.RouterRegistry.Handle("checkout.processpayment", (*controller.CheckoutController).ProcessPaymentAction)
	m.RouterRegistry.Route("/checkout/processpayment/:providercode/:methodcode", "checkout.processpayment")

	injector.BindMap((*paymentDomain.PaymentProvider)(nil), "offlinepayment").To(paymentInfrastructure.OfflinePaymentProvider{})

	injector.Bind((*form.Decoder)(nil)).ToProvider(form.NewDecoder).AsEagerSingleton()
	if m.UseFakeDeliveryLocationsService {
		injector.Override((*domain.SourcingService)(nil), "").To(infrastructure.FakeSourcingService{})
	}
}

// DefaultConfig
func (m *CheckoutModule) DefaultConfig() config.Map {
	return config.Map{
		"checkout": config.Map{
			"defaultPaymentMethod": "checkmo",
		},
	}
}
