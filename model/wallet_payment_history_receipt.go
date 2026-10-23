package model

// GetWalletCreditedReceipts fetches the page's settled payment evidence in one
// query. Second payments under review never inflate the original paid amount.
func GetWalletCreditedReceipts(ids []int) (map[int]WalletPaymentReceipt, error) {
	result := make(map[int]WalletPaymentReceipt, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	if len(ids) > 100 {
		return nil, ErrResellerFinanceInvalidInput
	}
	var receipts []WalletPaymentReceipt
	err := DB.Select("id", "top_up_id", "amount_minor", "currency", "currency_exponent").Where("top_up_id IN ? AND processing_status = ? AND verification_status = ?", ids, "credited", "verified").Order("id ASC").Find(&receipts).Error
	if err != nil {
		return nil, err
	}
	for _, receipt := range receipts {
		if receipt.TopUpID == nil {
			continue
		}
		if _, exists := result[*receipt.TopUpID]; !exists {
			result[*receipt.TopUpID] = receipt
		}
	}
	return result, nil
}
