package api

type SearchResponse struct {
	Data struct {
		SearchListingsByQuery struct {
			Listings struct {
				Edges []struct {
					Node Listing `json:"node"`
				} `json:"edges"`
				TotalCount int `json:"totalCount"`
			} `json:"listings"`
			SearchToken string `json:"searchToken"`
			Query       string `json:"query"`
		} `json:"searchListingsByQuery"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type Listing struct {
	ListingID      string `json:"listingID"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	FormattedPrice string `json:"formattedPrice"`
	Timestamp      string `json:"timestamp"`
	FormattedSource string `json:"formattedSource"`
	Highlighted    bool   `json:"highlighted"`
	PostcodeInformation struct {
		Postcode     string `json:"postcode"`
		LocationName string `json:"locationName"`
		Canton       struct {
			ShortName string `json:"shortName"`
			Name      string `json:"name"`
		} `json:"canton"`
	} `json:"postcodeInformation"`
	PrimaryCategory struct {
		CategoryID string `json:"categoryID"`
	} `json:"primaryCategory"`
	SellerInfo struct {
		Alias string `json:"alias"`
	} `json:"sellerInfo"`
	SEOInformation struct {
		DESlug string `json:"deSlug"`
	} `json:"seoInformation"`
	Images    []struct{} `json:"images"`
	Thumbnail struct {
		NormalRendition struct {
			Src string `json:"src"`
		} `json:"normalRendition"`
	} `json:"thumbnail"`
}

// NextDataResponse is the shape returned by the _next/data endpoint
type NextDataResponse struct {
	PageProps struct {
		DehydratedState struct {
			Queries []struct {
				QueryKey []interface{} `json:"queryKey"`
				State    struct {
					Data struct {
						SearchListingsByQuery struct {
							Listings struct {
								Edges []struct {
									Node Listing `json:"node"`
								} `json:"edges"`
								TotalCount int `json:"totalCount"`
							} `json:"listings"`
							SearchToken string `json:"searchToken"`
							Query       string `json:"query"`
						} `json:"searchListingsByQuery"`
					} `json:"data"`
				} `json:"state"`
			} `json:"queries"`
		} `json:"dehydratedState"`
	} `json:"pageProps"`
}
