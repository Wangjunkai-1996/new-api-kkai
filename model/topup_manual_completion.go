package model

func prepareManualTopUpCompletion(topUp *TopUp, _ *User) (TopUpCompletion, error) {
	var quotaDelta int64
	var err error
	switch topUp.PaymentProvider {
	case PaymentProviderEpay, PaymentProviderWaffo, PaymentProviderWaffoPancake:
		quotaDelta, err = quotaFromTopUpAmount(topUp.Amount)
	case PaymentProviderStripe:
		quotaDelta, err = quotaFromTopUpMoney(topUp.Money)
	case PaymentProviderCreem:
		quotaDelta, err = quotaFromTopUpCredits(topUp.Amount)
	default:
		return TopUpCompletion{}, ErrTopUpPaymentProviderInvalid
	}
	if err != nil {
		return TopUpCompletion{}, err
	}
	return TopUpCompletion{QuotaDelta: quotaDelta}, nil
}
