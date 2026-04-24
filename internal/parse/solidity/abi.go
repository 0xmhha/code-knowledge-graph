package solidity

// ABISig is a stripped-down function signature used by the cross-language
// linker (T20). We avoid pulling in solc; signatures are recovered from the
// AST per spec §4.6.3 ("ABI 추출 부산물"). ParamTypes is left nil in V0 since
// name-match is sufficient for the typechain-style binds_to heuristic.
type ABISig struct {
	ContractName string
	FunctionName string
	ParamTypes   []string
}
