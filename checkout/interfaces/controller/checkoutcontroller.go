package controller

import (
	"encoding/gob"
	"errors"
	"net/url"
	"strings"

	authApplication "go.aoe.com/flamingo/core/auth/application"
	canonicalApp "go.aoe.com/flamingo/core/canonicalUrl/application"
	cartApplication "go.aoe.com/flamingo/core/cart/application"
	"go.aoe.com/flamingo/core/cart/domain/cart"
	"go.aoe.com/flamingo/core/checkout/application"
	paymentDomain "go.aoe.com/flamingo/core/checkout/domain/payment"
	"go.aoe.com/flamingo/core/checkout/interfaces/controller/formDto"
	customerApplication "go.aoe.com/flamingo/core/customer/application"
	formApplicationService "go.aoe.com/flamingo/core/form/application"
	formDomain "go.aoe.com/flamingo/core/form/domain"
	"go.aoe.com/flamingo/framework/flamingo"
	"go.aoe.com/flamingo/framework/router"
	"go.aoe.com/flamingo/framework/web"
	"go.aoe.com/flamingo/framework/web/responder"
)

type (
	PaymentProviderProvider func() map[string]paymentDomain.PaymentProvider

	// CheckoutViewData represents the checkout view data
	CheckoutViewData struct {
		DecoratedCart        cart.DecoratedCart
		Form                 formDomain.Form
		CartValidationResult cart.CartValidationResult
		ErrorMessage         string
		HasSubmitError       bool
		PaymentProviders     map[string]paymentDomain.PaymentProvider
	}

	// SuccessViewData represents the success view data
	SuccessViewData struct {
		OrderId              string
		Email                string
		PlacedDecoratedItems []cart.DecoratedCartItem
		CartTotals           cart.CartTotals
	}

	// ReviewStepViewData represents the success view data
	ReviewStepViewData struct {
		DecoratedCart   cart.DecoratedCart
		SelectedPayment SelectedPayment
	}

	// SelectedPayment represents the success view data
	SelectedPayment struct {
		Provider string
		Method   string
	}

	// PlaceOrderFlashData represents the data passed to the success page - they need to be "glob"able
	PlaceOrderFlashData struct {
		OrderId string
		Email   string
		//Encodeable cart data to pass
		PlacedItems []cart.Item
		CartTotals  cart.CartTotals
	}

	// CheckoutController represents the checkout controller with its injectsions
	CheckoutController struct {
		responder.RenderAware   `inject:""`
		responder.RedirectAware `inject:""`
		Router                  *router.Router `inject:""`

		CheckoutFormService  *formDto.CheckoutFormService `inject:""`
		OrderService         *application.OrderService    `inject:""`
		PaymentService       *application.PaymentService  `inject:""`
		DecoratedCartFactory *cart.DecoratedCartFactory   `inject:""`

		SkipStartAction  bool `inject:"config:checkout.skipStartAction,optional"`
		SkipReviewAction bool `inject:"config:checkout.skipReviewAction,optional"`

		ApplicationCartService         *cartApplication.CartService         `inject:""`
		ApplicationCartReceiverService *cartApplication.CartReceiverService `inject:""`

		UserService *authApplication.UserService `inject:""`

		Logger flamingo.Logger `inject:""`

		CustomerApplicationService *customerApplication.Service `inject:""`
		PaymentProvider            PaymentProviderProvider      `inject:""`

		CanonicalService *canonicalApp.Service `inject:""`
	}
)

func init() {
	gob.Register(PlaceOrderFlashData{})
}

/*
The checkoutController implements a default process for a checkout:
 * StartAction (supposed to show a switch to go to guest or customer)
 	* can be skipped with a configuration
 * SubmitUserCheckoutAction  OR  SubmitGuestCheckoutAction
 	* both actions are more or less the same (User checkout just populates the customer to the form and uses a different template)
 	* This step is supposed to show a big form (validation and default values are configurable as well)
	* payment can be selected in this step or in the next

 * ReviewAction
	* this step is supposed to show the current cart status just before checkout
		* optional the paymentmethod can also be selected here
	* This step can also be skipped - then directly the placeOrder is handled

*  Optional Payment Step (if the payment requires a redirect the payment page is shown and a redirect back to "ProcessPayment"
* SuccessStep


*/

// StartAction handles the checkout start action
func (cc *CheckoutController) StartAction(ctx web.Context) web.Response {
	//Guard Clause if Cart cannout be fetched

	decoratedCart, e := cc.ApplicationCartReceiverService.ViewDecoratedCart(ctx)
	if e != nil {
		cc.Logger.WithField("category", "checkout").Errorf("cart.checkoutcontroller.viewaction: Error %v", e)
		return cc.Render(ctx, "checkout/carterror", nil)
	}

	//Guard Clause if Cart is empty
	if decoratedCart.Cart.ItemCount() == 0 {
		return cc.Render(ctx, "checkout/startcheckout", CheckoutViewData{
			DecoratedCart: *decoratedCart,
		})
	}

	if cc.UserService.IsLoggedIn(ctx) {
		return cc.Redirect("checkout.user", nil)
	}

	if cc.SkipStartAction {
		return cc.Redirect("checkout.guest", nil)
	}

	return cc.Render(ctx, "checkout/startcheckout", CheckoutViewData{
		DecoratedCart: *decoratedCart,
	})
}

func (cc *CheckoutController) hasAvailablePaymentProvider() bool {
	return len(cc.getPaymentProviders()) > 0
}

func (cc *CheckoutController) getPayment(ctx web.Context, paymentProviderCode string, paymentMethodCode string) (paymentDomain.PaymentProvider, *paymentDomain.PaymentMethod, error) {
	providers := cc.getPaymentProviders()

	provider := providers[paymentProviderCode]

	if provider == nil {
		return nil, nil, errors.New("Payment provider " + paymentProviderCode + " not found")
	}

	paymentMethods := provider.GetPaymentMethods()

	var paymentMethod *paymentDomain.PaymentMethod
	for _, method := range paymentMethods {
		if method.Code == paymentMethodCode {
			paymentMethod = &method
			break
		}
	}

	if paymentMethod == nil {
		return nil, nil, errors.New("payment method not found")
	}
	return provider, paymentMethod, nil
}

// SubmitUserCheckoutAction handles the user order submit
func (cc *CheckoutController) SubmitUserCheckoutAction(ctx web.Context) web.Response {
	//Guard
	if !cc.UserService.IsLoggedIn(ctx) {
		return cc.Redirect("checkout.start", nil)
	}
	customer, err := cc.CustomerApplicationService.GetForAuthenticatedUser(ctx)
	if err == nil {
		//give the customer to the form service - so that it can prepopulate default values
		cc.CheckoutFormService.Customer = customer
	}

	return cc.showCheckoutFormAndHandleSubmit(ctx, cc.CheckoutFormService, "checkout/usercheckout")
}

// SubmitGuestCheckoutAction handles the guest order submit
func (cc *CheckoutController) SubmitGuestCheckoutAction(ctx web.Context) web.Response {
	cc.CheckoutFormService.Customer = nil
	if cc.UserService.IsLoggedIn(ctx) {
		return cc.Redirect("checkout.user", nil)
	}
	return cc.showCheckoutFormAndHandleSubmit(ctx, cc.CheckoutFormService, "checkout/guestcheckout")
}

// ProcessPaymentAction functions as a return/notification URL for Payment Providers
func (cc *CheckoutController) ProcessPaymentAction(ctx web.Context) web.Response {

	//Guard Clause if Cart cannout be fetched
	decoratedCart, e := cc.ApplicationCartReceiverService.ViewDecoratedCart(ctx)
	if e != nil {
		cc.Logger.WithField("category", "checkout").Errorf("cart.checkoutcontroller.submitaction: Error %v", e)
		return cc.Render(ctx, "checkout/carterror", nil)
	}

	providercode := ctx.MustParam1("providercode")
	methodcode := ctx.MustParam1("methodcode")

	email := "todo"

	provider, paymentMethod, err := cc.getPayment(ctx, providercode, methodcode)

	cartPayment, err := provider.ProcessPayment(ctx, &decoratedCart.Cart, paymentMethod, nil)
	if err != nil {
		return cc.placeOrderErrorResponse(ctx, "", *decoratedCart, nil, err)
	}

	response, err := cc.placeOrder(ctx, *cartPayment, email, *decoratedCart)
	if err != nil {
		return cc.placeOrderErrorResponse(ctx, "", *decoratedCart, nil, err)
	}
	return response
}

// SuccessAction handles the order success action
func (cc *CheckoutController) SuccessAction(ctx web.Context) web.Response {
	flashes := ctx.Session().Flashes("checkout.success.data")
	if len(flashes) > 0 {
		if placeOrderFlashData, ok := flashes[0].(PlaceOrderFlashData); ok {
			viewData := SuccessViewData{
				CartTotals:           placeOrderFlashData.CartTotals,
				Email:                placeOrderFlashData.Email,
				OrderId:              placeOrderFlashData.OrderId,
				PlacedDecoratedItems: cc.DecoratedCartFactory.CreateDecorateCartItems(ctx, placeOrderFlashData.PlacedItems),
			}
			return cc.Render(ctx, "checkout/success", viewData)
		}
	}

	return cc.Render(ctx, "checkout/expired", nil)
}

func (cc *CheckoutController) getPaymentReturnUrl(PaymentProvider string, PaymentMethod string) *url.URL {
	baseUrl := cc.CanonicalService.BaseUrl
	paymentUrl := cc.Router.URL("checkout.processpayment", router.P{"providercode": PaymentProvider, "methodcode": PaymentMethod})

	rawUrl := strings.TrimRight(baseUrl, "/") + paymentUrl.String()

	urlResult, _ := url.Parse(rawUrl)

	return urlResult
}

//showCheckoutFormAndHandleSubmit - Action that shows the form (either customer or guest)
func (cc *CheckoutController) showCheckoutFormAndHandleSubmit(ctx web.Context, formservice *formDto.CheckoutFormService, template string) web.Response {

	//Guard Clause if Cart cannout be fetched
	decoratedCart, e := cc.ApplicationCartReceiverService.ViewDecoratedCart(ctx)
	if e != nil {
		cc.Logger.WithField("category", "checkout").Errorf("cart.checkoutcontroller.submitaction: Error %v", e)
		return cc.Render(ctx, "checkout/carterror", nil)
	}

	if formservice == nil {
		cc.Logger.WithField("category", "checkout").Error("cart.checkoutcontroller.submitaction: Error CheckoutFormService not present!")
		return cc.Render(ctx, "checkout/carterror", nil)
	}

	if !cc.hasAvailablePaymentProvider() {
		cc.Logger.WithField("category", "checkout").Error("cart.checkoutcontroller.submitaction: Error No Payment set")
		return cc.Render(ctx, "checkout/carterror", nil)
	}

	form, e := formApplicationService.ProcessFormRequest(ctx, formservice)
	// return on error (template need to handle error display)
	if e != nil {
		return cc.Render(ctx, template, CheckoutViewData{
			DecoratedCart:        *decoratedCart,
			CartValidationResult: cc.ApplicationCartService.ValidateCart(ctx, decoratedCart),
			Form:                 form,
			PaymentProviders:     cc.getPaymentProviders(),
		})
	}

	//Guard Clause if Cart is empty
	if decoratedCart.Cart.ItemCount() == 0 {
		return cc.Render(ctx, template, CheckoutViewData{
			DecoratedCart:        *decoratedCart,
			CartValidationResult: cc.ApplicationCartService.ValidateCart(ctx, decoratedCart),
			Form:                 form,
			PaymentProviders:     cc.getPaymentProviders(),
		})
	}

	if form.IsValidAndSubmitted() {

		if checkoutFormData, ok := form.Data.(formDto.CheckoutFormData); ok {

			billingAddress, shippingAddress := formDto.MapAddresses(checkoutFormData)
			person := formDto.MapPerson(checkoutFormData)

			err := cc.OrderService.CurrentCartSaveInfos(ctx, billingAddress, shippingAddress, person)
			if err != nil {
				return cc.placeOrderErrorResponse(ctx, template, *decoratedCart, &form, err)
			}

			if cc.SkipReviewAction {
				return cc.processPaymentOrPlaceOrderDirectly(ctx, checkoutFormData.SelectedPaymentProvider, checkoutFormData.SelectedPaymentProviderMethod, decoratedCart, template, &form)
			}
			return cc.Redirect("checkout.review", nil)
		} else {
			cc.Logger.WithField("category", "checkout").Error("cart.checkoutcontroller.submitaction: Error cannot type convert to CheckoutFormData!")
			return cc.Render(ctx, "checkout/carterror", nil)
		}
	} else {
		if form.IsSubmitted && form.HasGeneralErrors() {
			cc.Logger.WithField("category", "checkout").Warnf("CheckoutForm has general error: %#v", form.ValidationInfo.GeneralErrors)
		}
	}

	cc.Logger.Debugf("paymentProviders %#v", cc.getPaymentProviders())
	//Default: Form not submitted yet or submitted with validation errors:
	return cc.Render(ctx, template, CheckoutViewData{
		DecoratedCart:        *decoratedCart,
		CartValidationResult: cc.ApplicationCartService.ValidateCart(ctx, decoratedCart),
		Form:                 form,
		PaymentProviders:     cc.getPaymentProviders(),
	})
}

//placeOrderErrorResponse - error handling that is called form many places... It will show the checkoutform and the error
// template and form is optional - if it is not goven it is autodetected and prefilled from the infos in the cart
func (cc *CheckoutController) placeOrderErrorResponse(ctx web.Context, template string, decoratedCart cart.DecoratedCart, form *formDomain.Form, err error) web.Response {
	if template == "" {
		template = "checkout/guestcheckout"
		if !cc.UserService.IsLoggedIn(ctx) {
			template = "checkout/usercheckout"
		}
	}
	cc.Logger.Warnf("Place Order Error: %s", err.Error())
	if form == nil {
		cc.CheckoutFormService.PrefillFormFromCart = true
		cc.CheckoutFormService.Cart = &decoratedCart.Cart
		newForm, _ := formApplicationService.ProcessFormRequest(ctx, cc.CheckoutFormService)
		form = &newForm
	}

	return cc.Render(ctx, template, CheckoutViewData{
		DecoratedCart:        decoratedCart,
		CartValidationResult: cc.ApplicationCartService.ValidateCart(ctx, &decoratedCart),
		HasSubmitError:       true,
		Form:                 *form,
		ErrorMessage:         err.Error(),
		PaymentProviders:     cc.getPaymentProviders(),
	})

}

func (cc *CheckoutController) processPaymentOrPlaceOrderDirectly(ctx web.Context, selectedPaymentProvider string, selectedPaymentProviderMethod string, decoratedCart *cart.DecoratedCart, orderFormTemplate string, checkoutForm *formDomain.Form) web.Response {
	//procces Payment:
	paymentProvider, paymentMethod, err := cc.getPayment(ctx, selectedPaymentProvider, selectedPaymentProviderMethod)
	if err != nil {
		return cc.placeOrderErrorResponse(ctx, orderFormTemplate, *decoratedCart, checkoutForm, err)
	}
	//Payment Method requests an redirect - execute it
	if paymentMethod.IsExternalPayment {
		returnUrl := cc.getPaymentReturnUrl(paymentProvider.GetCode(), paymentMethod.Code)
		hostedPaymentPageResponse, err := paymentProvider.RedirectExternalPayment(ctx, &decoratedCart.Cart, paymentMethod, returnUrl)
		if err != nil {
			return cc.placeOrderErrorResponse(ctx, orderFormTemplate, *decoratedCart, checkoutForm, err)
		}
		return hostedPaymentPageResponse
	}

	//Paymentmethod that need no external Redirect - can be processed directly
	cartPayment, err := paymentProvider.ProcessPayment(ctx, &decoratedCart.Cart, paymentMethod, nil)
	if err != nil {
		return cc.placeOrderErrorResponse(ctx, orderFormTemplate, *decoratedCart, checkoutForm, err)
	}
	shippingEmail := decoratedCart.Cart.GetMainShippingEMail()
	if shippingEmail == "" {
		shippingEmail = decoratedCart.Cart.BillingAdress.Email
	}
	response, err := cc.placeOrder(ctx, *cartPayment, shippingEmail, *decoratedCart)
	if err != nil {
		return cc.placeOrderErrorResponse(ctx, orderFormTemplate, *decoratedCart, checkoutForm, err)
	}
	return response
}

func (cc *CheckoutController) placeOrder(ctx web.Context, cartPayment cart.CartPayment, email string, decoratedCart cart.DecoratedCart) (web.Response, error) {
	orderID, err := cc.OrderService.CurrentCartPlaceOrder(ctx, cartPayment)
	if err != nil {
		return nil, err
	}

	return cc.Redirect("checkout.success", nil).With("checkout.success.data", PlaceOrderFlashData{
		OrderId:     orderID,
		Email:       email,
		PlacedItems: decoratedCart.Cart.Cartitems,
		CartTotals:  decoratedCart.Cart.CartTotals,
	}), nil

}
func (cc *CheckoutController) getPaymentProviders() map[string]paymentDomain.PaymentProvider {
	result := make(map[string]paymentDomain.PaymentProvider)

	paymentProviders := cc.PaymentProvider()

	if paymentProviders != nil {
		for name, paymentProvider := range cc.PaymentProvider() {
			if paymentProvider.IsActive() {
				result[name] = paymentProvider
			}
		}
	}

	return result
}

// ReviewAction
func (cc *CheckoutController) ReviewAction(ctx web.Context) web.Response {

	selectedProvider, _ := ctx.Form1("selectedPaymentProvider")
	selectedMethod, _ := ctx.Form1("selectedPaymentProviderMethod")
	proceed, _ := ctx.Form1("proceed")
	termsAndConditions, _ := ctx.Form1("termsAndConditions")

	cc.Logger.Debugf("ReviewAction: selectedProvider: %v / selectedMethod: %v / proceed: %v / termsAndConditions: %v", selectedProvider, selectedMethod, proceed, termsAndConditions)

	//Guard Clause if Cart cannout be fetched
	decoratedCart, e := cc.ApplicationCartReceiverService.ViewDecoratedCart(ctx)
	if e != nil {
		cc.Logger.WithField("category", "checkout").Errorf("cart.checkoutcontroller.submitaction: Error %v", e)
		return cc.Render(ctx, "checkout/carterror", nil)
	}

	if proceed == "1" && termsAndConditions == "1" && selectedProvider != "" && selectedMethod != "" {
		return cc.processPaymentOrPlaceOrderDirectly(ctx, selectedProvider, selectedMethod, decoratedCart, "", nil)
	}

	viewData := ReviewStepViewData{
		DecoratedCart: *decoratedCart,
		SelectedPayment: SelectedPayment{
			Provider: selectedProvider,
			Method:   selectedMethod,
		},
	}
	return cc.Render(ctx, "checkout/review", viewData)

}
