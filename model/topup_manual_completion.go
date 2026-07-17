package model

import "github.com/QuantumNous/new-api/common"

func prepareManualTopUpCompletion(topUp *TopUp, _ *User) (TopUpCompletion, error) {
	var quotaDelta int64
	var err error
	switch topUp.PaymentProvider {
	case PaymentProviderEpay, PaymentProviderWaffo, PaymentProviderWaffoPancake:
		quotaDelta, err = quotaFromTopUpAmount(topUp.Amount)
	case PaymentProviderStripe:
		quotaDelta, err = quotaFromTopUpMoney(topUp.Money)
	case PaymentProviderCreem:
		quotaDelta = topUp.Amount
		if quotaDelta <= 0 || quotaDelta > int64(common.MaxQuota) {
			err = ErrTopUpQuotaInvalid
		}
	default:
		return TopUpCompletion{}, ErrTopUpPaymentProviderInvalid
	}
	if err != nil {
		return TopUpCompletion{}, err
	}
	return TopUpCompletion{QuotaDelta: quotaDelta}, nil
}
