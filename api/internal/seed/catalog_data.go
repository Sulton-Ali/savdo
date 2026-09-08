package seed

import (
	"fmt"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// unitSpec is one of the four units of measure Catalog upserts
// (docs/04-DATA-MODEL.md § 2: "Seeded per shop").
type unitSpec struct {
	code       string
	precision  int16
	uz, ru, en string
}

var unitSpecs = []unitSpec{
	{code: "pcs", precision: 0, uz: "Dona", ru: "Штука", en: "Piece"},
	{code: "kg", precision: 3, uz: "Kilogramm", ru: "Килограмм", en: "Kilogram"},
	{code: "m", precision: 0, uz: "Metr", ru: "Метр", en: "Meter"},
	{code: "l", precision: 3, uz: "Litr", ru: "Литр", en: "Liter"},
}

// attributeSpec is one of the two attribute definitions Catalog
// creates (Q-03: size + colour for the clothing shop).
type attributeSpec struct {
	code       string
	sortOrder  int
	uz, ru, en string
}

var attributeSpecs = []attributeSpec{
	{code: "size", sortOrder: 1, uz: "O‘lcham", ru: "Размер", en: "Size"},
	{code: "color", sortOrder: 2, uz: "Rang", ru: "Цвет", en: "Color"},
}

// translations3 builds a gen.Translations with all three of the shop's
// supported locales, the shape every catalog Create body needs.
func translations3(uz, ru, en string) gen.Translations {
	return gen.Translations{
		Uz: &gen.TranslationEntry{Name: uz},
		Ru: &gen.TranslationEntry{Name: ru},
		En: &gen.TranslationEntry{Name: en},
	}
}

// translations3Desc is translations3 plus a description in every locale,
// for products.
func translations3Desc(uz, ru, en, descUz, descRu, descEn string) gen.Translations {
	return gen.Translations{
		Uz: &gen.TranslationEntry{Name: uz, Description: &descUz},
		Ru: &gen.TranslationEntry{Name: ru, Description: &descRu},
		En: &gen.TranslationEntry{Name: en, Description: &descEn},
	}
}

// categorySpec is one category Catalog creates. parentSlug is ""
// for a top-level category; categorySpecs lists parents before their
// children, since seedCategories resolves parentSlug against
// categories it has already created earlier in this same slice.
type categorySpec struct {
	slug, parentSlug string
	sortOrder        int
	uz, ru, en       string
}

var categorySpecs = []categorySpec{
	{slug: "men", sortOrder: 1, uz: "Erkaklar", ru: "Мужчины", en: "Men"},
	{slug: "women", sortOrder: 2, uz: "Ayollar", ru: "Женщины", en: "Women"},
	{slug: "kids", sortOrder: 3, uz: "Bolalar", ru: "Дети", en: "Kids"},

	{slug: "men-shirts", parentSlug: "men", sortOrder: 1, uz: "Ko‘ylaklar", ru: "Рубашки", en: "Shirts"},
	{slug: "men-trousers", parentSlug: "men", sortOrder: 2, uz: "Shimlar", ru: "Брюки", en: "Trousers"},
	{slug: "men-jackets", parentSlug: "men", sortOrder: 3, uz: "Kurtkalar", ru: "Куртки", en: "Jackets"},

	{slug: "women-dresses", parentSlug: "women", sortOrder: 1, uz: "Ko‘ylaklar", ru: "Платья", en: "Dresses"},
	{slug: "women-blouses", parentSlug: "women", sortOrder: 2, uz: "Bluzkalar", ru: "Блузки", en: "Blouses"},
}

// variantSpec is one variant Catalog attaches to a product, before it
// has an id: size/colour attribute values, an optional price override
// ("" for none) and whether it is active.
type variantSpec struct {
	size, color   string
	priceOverride string
	isActive      bool
}

// buildVariants zips sizes and colors into count variant specs, cycling
// through both lists (size fastest) so every pair is unique as long as
// count <= len(sizes)*len(colors) — true for every call site below.
// overridePriceIdx and inactiveIdx key by position in the returned slice
// (0-based), letting a handful of seeded products carry a price override
// or an inactive variant (task spec: "a few price overrides ... a couple
// inactive").
func buildVariants(sizes, colors []string, count int, overridePriceIdx map[int]string, inactiveIdx map[int]bool) []variantSpec {
	out := make([]variantSpec, count)
	for i := range count {
		v := variantSpec{size: sizes[i%len(sizes)], color: colors[(i/len(sizes))%len(colors)], isActive: true}
		if p, ok := overridePriceIdx[i]; ok {
			v.priceOverride = p
		}
		if inactiveIdx[i] {
			v.isActive = false
		}
		out[i] = v
	}
	return out
}

// imageSpec is one placeholder image Catalog generates and attaches to
// a product. variantIdx >= 0 ties the image to that position in the
// product's own (just-created) variant list; -1 means a plain product-level
// image.
type imageSpec struct {
	bgHex, label string
	variantIdx   int
}

// productImages builds count imageSpecs sharing one background/label; when
// tagVariant is true, the second image (if any) is tied to the product's
// first variant — "some tagged to a variant" (task spec) without every
// product needing one. Each image's label carries its own 1-based index
// ("Label #2"), so a product with more than one image never generates two
// byte-identical placeholders: media.Service.Upload dedupes by sha256, and
// two identical uploads for the same product would collide on
// product_images' (product_id, media_id) unique constraint on the second
// AddProductImage call.
func productImages(count int, bgHex, label string, tagVariant bool) []imageSpec {
	imgs := make([]imageSpec, count)
	for i := range count {
		variantIdx := -1
		if tagVariant && i == 1 {
			variantIdx = 0
		}
		imgLabel := label
		if count > 1 {
			imgLabel = fmt.Sprintf("%s #%d", label, i+1)
		}
		imgs[i] = imageSpec{bgHex: bgHex, label: imgLabel, variantIdx: variantIdx}
	}
	return imgs
}

// productSpec is one product Catalog creates, with its variants and
// images already resolved to concrete specs.
type productSpec struct {
	slug, categorySlug, sku string
	nameUz, nameRu, nameEn  string
	descUz, descRu, descEn  string
	basePrice, costPrice    string
	promoPrice              string // "" for no promo
	isFeatured              bool   // D-101: curated manually, a handful of the demo catalogue
	variants                []variantSpec
	images                  []imageSpec
}

// Colour attribute values (Uzbek, the shop's default locale — attribute
// *values* have no translation table, only the attribute *definition*
// does, docs/04-DATA-MODEL.md § 2). ~10 colours per the task spec.
var (
	cQora     = "qora"      // black
	cOq       = "oq"        // white
	cKok      = "kok"       // blue
	cQizil    = "qizil"     // red
	cYashil   = "yashil"    // green
	cSariq    = "sariq"     // yellow
	cKulrang  = "kulrang"   // gray
	cJigar    = "jigarrang" // brown
	cPushti   = "pushti"    // pink
	cBinafsha = "binafsha"  // purple
)

var (
	sizesXStoXL  = []string{"XS", "S", "M", "L", "XL"}
	sizesStoXXL  = []string{"S", "M", "L", "XL", "XXL"}
	sizesStoL    = []string{"S", "M", "L"}
	trouserSizes = []string{"28", "30", "32", "34", "36"}
	kidsSizes    = []string{"XS", "S", "M"}
)

// productSpecs is the ~30-product seeded clothing catalogue: 5 products
// in each of the 6 leaf categories, spanning realistic UZS price
// magnitudes, several with a promo price, a few variants with a price
// override, a couple of inactive variants, and 1-4 generated images per
// product (some tagged to a variant).
var productSpecs = []productSpec{
	// Men / Shirts
	{
		slug: "men-shirt-classic-white", categorySlug: "men-shirts", sku: "MSH-01",
		nameUz: "Klassik oq ko‘ylak", nameRu: "Классическая белая рубашка", nameEn: "Classic white shirt",
		descUz: "Ofis va rasmiy tadbirlar uchun paxta ko‘ylak.", descRu: "Хлопковая рубашка для офиса и торжественных случаев.", descEn: "Cotton shirt for the office and formal occasions.",
		basePrice: "189000.00", costPrice: "95000.00", isFeatured: true,
		variants: buildVariants(sizesXStoXL, []string{cOq, cKok}, 6, map[int]string{0: "199000.00"}, nil),
		images:   productImages(2, "1f6feb", "Classic White Shirt", true),
	},
	{
		slug: "men-shirt-checked", categorySlug: "men-shirts", sku: "MSH-02",
		nameUz: "Katakli ko‘ylak", nameRu: "Клетчатая рубашка", nameEn: "Checked shirt",
		descUz: "Kundalik kiyish uchun katakli naqshli ko‘ylak.", descRu: "Рубашка в клетку для повседневной носки.", descEn: "Checked-pattern shirt for everyday wear.",
		basePrice: "219000.00", costPrice: "110000.00",
		variants: buildVariants(sizesStoXXL, []string{cKok, cQizil, cKulrang}, 5, nil, nil),
		images:   productImages(3, "d1242f", "Checked Shirt", false),
	},
	{
		slug: "men-shirt-linen", categorySlug: "men-shirts", sku: "MSH-03",
		nameUz: "Zig‘ir ko‘ylak", nameRu: "Льняная рубашка", nameEn: "Linen shirt",
		descUz: "Yozgi issiq kunlar uchun yengil zig‘ir mato.", descRu: "Лёгкая льняная ткань для жарких летних дней.", descEn: "Lightweight linen for hot summer days.",
		basePrice: "259000.00", costPrice: "140000.00", promoPrice: "219000.00",
		variants: buildVariants(sizesStoL, []string{cOq, cSariq}, 4, nil, nil),
		images:   productImages(2, "bf8700", "Linen Shirt", false),
	},
	{
		slug: "men-shirt-denim", categorySlug: "men-shirts", sku: "MSH-04",
		nameUz: "Jinsi ko‘ylak", nameRu: "Джинсовая рубашка", nameEn: "Denim shirt",
		descUz: "Bardoshli denim matodan tikilgan ko‘ylak.", descRu: "Рубашка из прочной джинсовой ткани.", descEn: "Shirt cut from durable denim fabric.",
		basePrice: "239000.00", costPrice: "125000.00",
		variants: buildVariants(sizesXStoXL, []string{cKok}, 3, nil, map[int]bool{2: true}),
		images:   productImages(2, "1a7f37", "Denim Shirt", true),
	},
	{
		slug: "men-shirt-slimfit", categorySlug: "men-shirts", sku: "MSH-05",
		nameUz: "Slim fit ko‘ylak", nameRu: "Приталенная рубашка", nameEn: "Slim fit shirt",
		descUz: "Tanaga yopishib turadigan zamonaviy fason.", descRu: "Приталенный крой современного силуэта.", descEn: "A close, modern-cut silhouette.",
		basePrice: "209000.00", costPrice: "108000.00",
		variants: buildVariants(sizesStoXXL, []string{cOq, cKulrang}, 4, nil, nil),
		images:   productImages(1, "0969da", "Slim Fit Shirt", false),
	},

	// Men / Trousers (numeric sizes)
	{
		slug: "men-trousers-classic", categorySlug: "men-trousers", sku: "MTR-01",
		nameUz: "Klassik shim", nameRu: "Классические брюки", nameEn: "Classic trousers",
		descUz: "Har kungi va ofis kiyimlari uchun klassik shim.", descRu: "Классические брюки для офиса и на каждый день.", descEn: "Classic trousers for the office and everyday.",
		basePrice: "289000.00", costPrice: "150000.00",
		variants: buildVariants(trouserSizes, []string{cQora, cKulrang}, 6, map[int]string{0: "299000.00"}, nil),
		images:   productImages(2, "24292f", "Classic Trousers", true),
	},
	{
		slug: "men-trousers-chino", categorySlug: "men-trousers", sku: "MTR-02",
		nameUz: "Chino shim", nameRu: "Брюки чинос", nameEn: "Chino trousers",
		descUz: "Yengil paxta matodan chino fasonli shim.", descRu: "Брюки чинос из лёгкой хлопковой ткани.", descEn: "Chino-cut trousers in lightweight cotton.",
		basePrice: "259000.00", costPrice: "135000.00",
		variants: buildVariants(trouserSizes, []string{cKulrang, cSariq}, 5, nil, nil),
		images:   productImages(2, "bf8700", "Chino Trousers", false),
	},
	{
		slug: "men-trousers-jeans", categorySlug: "men-trousers", sku: "MTR-03",
		nameUz: "Jinsi shim", nameRu: "Джинсы", nameEn: "Jeans",
		descUz: "Klassik ko‘k denim jinsi shim.", descRu: "Классические синие джинсы из денима.", descEn: "Classic blue denim jeans.",
		basePrice: "279000.00", costPrice: "145000.00", promoPrice: "239000.00",
		variants: buildVariants(trouserSizes, []string{cKok}, 5, nil, nil),
		images:   productImages(3, "1f6feb", "Jeans", true),
	},
	{
		slug: "men-trousers-cargo", categorySlug: "men-trousers", sku: "MTR-04",
		nameUz: "Karg shim", nameRu: "Брюки карго", nameEn: "Cargo trousers",
		descUz: "Yon cho‘ntaklari ko‘p bo‘lgan amaliy shim.", descRu: "Практичные брюки с накладными карманами.", descEn: "Practical trousers with extra cargo pockets.",
		basePrice: "299000.00", costPrice: "160000.00",
		variants: buildVariants(trouserSizes, []string{cKulrang, cYashil}, 4, nil, map[int]bool{3: true}),
		images:   productImages(2, "1a7f37", "Cargo Trousers", false),
	},
	{
		slug: "men-trousers-formal", categorySlug: "men-trousers", sku: "MTR-05",
		nameUz: "Rasmiy shim", nameRu: "Выходные брюки", nameEn: "Formal trousers",
		descUz: "Tantanali tadbirlar uchun rasmiy shim.", descRu: "Строгие брюки для торжественных случаев.", descEn: "Formal trousers for special occasions.",
		basePrice: "319000.00", costPrice: "170000.00",
		variants: buildVariants(trouserSizes, []string{cQora}, 5, nil, nil),
		images:   productImages(1, "24292f", "Formal Trousers", false),
	},

	// Men / Jackets
	{
		slug: "men-jacket-leather", categorySlug: "men-jackets", sku: "MJK-01",
		nameUz: "Charm kurtka", nameRu: "Кожаная куртка", nameEn: "Leather jacket",
		descUz: "Haqiqiy charmdan tikilgan qishki kurtka.", descRu: "Куртка из натуральной кожи.", descEn: "Jacket cut from genuine leather.",
		basePrice: "1290000.00", costPrice: "850000.00", isFeatured: true,
		variants: buildVariants(sizesStoXXL, []string{cQora, cJigar}, 5, map[int]string{0: "1350000.00"}, nil),
		images:   productImages(4, "24292f", "Leather Jacket", true),
	},
	{
		slug: "men-jacket-denim", categorySlug: "men-jackets", sku: "MJK-02",
		nameUz: "Jinsi kurtka", nameRu: "Джинсовая куртка", nameEn: "Denim jacket",
		descUz: "Har qanday kiyimga mos denim kurtka.", descRu: "Джинсовая куртка на каждый день.", descEn: "A denim jacket that goes with anything.",
		basePrice: "459000.00", costPrice: "240000.00",
		variants: buildVariants(sizesStoXXL, []string{cKok}, 4, nil, nil),
		images:   productImages(2, "1f6feb", "Denim Jacket", false),
	},
	{
		slug: "men-jacket-winter", categorySlug: "men-jackets", sku: "MJK-03",
		nameUz: "Qishki kurtka", nameRu: "Зимняя куртка", nameEn: "Winter jacket",
		descUz: "Sovuq kunlar uchun issiq to‘ldirgichli kurtka.", descRu: "Утеплённая куртка для холодных дней.", descEn: "Insulated jacket for cold weather.",
		basePrice: "890000.00", costPrice: "520000.00", promoPrice: "750000.00",
		variants: buildVariants(sizesStoXXL, []string{cQora, cKulrang, cKok}, 6, nil, nil),
		images:   productImages(3, "0969da", "Winter Jacket", true),
	},
	{
		slug: "men-jacket-bomber", categorySlug: "men-jackets", sku: "MJK-04",
		nameUz: "Bomber kurtka", nameRu: "Куртка бомбер", nameEn: "Bomber jacket",
		descUz: "Sport uslubidagi bomber kurtka.", descRu: "Куртка бомбер в спортивном стиле.", descEn: "A sport-style bomber jacket.",
		basePrice: "529000.00", costPrice: "290000.00",
		variants: buildVariants(sizesStoXXL, []string{cQora, cYashil}, 4, nil, nil),
		images:   productImages(2, "1a7f37", "Bomber Jacket", false),
	},
	{
		slug: "men-jacket-sport", categorySlug: "men-jackets", sku: "MJK-05",
		nameUz: "Sport kurtka", nameRu: "Спортивная куртка", nameEn: "Sport jacket",
		descUz: "Yengil sport kurtka, mashg‘ulot uchun qulay.", descRu: "Лёгкая спортивная куртка для тренировок.", descEn: "A light sport jacket comfortable for training.",
		basePrice: "399000.00", costPrice: "210000.00",
		variants: buildVariants(sizesStoXXL, []string{cKulrang, cQizil}, 4, nil, nil),
		images:   productImages(1, "cf222e", "Sport Jacket", false),
	},

	// Women / Dresses
	{
		slug: "women-dress-summer", categorySlug: "women-dresses", sku: "WDR-01",
		nameUz: "Yozgi ko‘ylak", nameRu: "Летнее платье", nameEn: "Summer dress",
		descUz: "Yengil va salqin yozgi ko‘ylak.", descRu: "Лёгкое и прохладное летнее платье.", descEn: "A light, breezy summer dress.",
		basePrice: "259000.00", costPrice: "130000.00", promoPrice: "219000.00", isFeatured: true,
		variants: buildVariants(sizesXStoXL, []string{cSariq, cPushti}, 6, nil, nil),
		images:   productImages(3, "bf8700", "Summer Dress", true),
	},
	{
		slug: "women-dress-evening", categorySlug: "women-dresses", sku: "WDR-02",
		nameUz: "Kechki ko‘ylak", nameRu: "Вечернее платье", nameEn: "Evening dress",
		descUz: "Tantanali kechalar uchun nafis ko‘ylak.", descRu: "Элегантное платье для торжественных вечеров.", descEn: "An elegant dress for evening occasions.",
		basePrice: "890000.00", costPrice: "550000.00",
		variants: buildVariants(sizesXStoXL, []string{cQora, cBinafsha}, 5, map[int]string{0: "950000.00"}, nil),
		images:   productImages(4, "6639ba", "Evening Dress", true),
	},
	{
		slug: "women-dress-casual", categorySlug: "women-dresses", sku: "WDR-03",
		nameUz: "Kundalik ko‘ylak", nameRu: "Повседневное платье", nameEn: "Casual dress",
		descUz: "Har kungi kiyish uchun qulay ko‘ylak.", descRu: "Удобное платье на каждый день.", descEn: "A comfortable dress for everyday wear.",
		basePrice: "199000.00", costPrice: "100000.00",
		variants: buildVariants(sizesXStoXL, []string{cKok, cKulrang}, 4, nil, nil),
		images:   productImages(2, "0969da", "Casual Dress", false),
	},
	{
		slug: "women-dress-floral", categorySlug: "women-dresses", sku: "WDR-04",
		nameUz: "Gulli ko‘ylak", nameRu: "Платье с цветочным принтом", nameEn: "Floral dress",
		descUz: "Gulli naqshli yengil ko‘ylak.", descRu: "Лёгкое платье с цветочным узором.", descEn: "A light dress with a floral print.",
		basePrice: "229000.00", costPrice: "115000.00",
		variants: buildVariants(sizesXStoXL, []string{cPushti, cSariq}, 4, nil, map[int]bool{3: true}),
		images:   productImages(2, "d1242f", "Floral Dress", false),
	},
	{
		slug: "women-dress-midi", categorySlug: "women-dresses", sku: "WDR-05",
		nameUz: "Midi ko‘ylak", nameRu: "Платье миди", nameEn: "Midi dress",
		descUz: "O‘rta uzunlikdagi zamonaviy ko‘ylak.", descRu: "Современное платье средней длины.", descEn: "A modern, midi-length dress.",
		basePrice: "279000.00", costPrice: "145000.00",
		variants: buildVariants(sizesXStoXL, []string{cQora, cKok}, 5, nil, nil),
		images:   productImages(1, "24292f", "Midi Dress", false),
	},

	// Women / Blouses
	{
		slug: "women-blouse-silk", categorySlug: "women-blouses", sku: "WBL-01",
		nameUz: "Ipak bluzka", nameRu: "Шёлковая блузка", nameEn: "Silk blouse",
		descUz: "Nafis ipak matodan bluzka.", descRu: "Блузка из благородного шёлка.", descEn: "A blouse in fine silk fabric.",
		basePrice: "249000.00", costPrice: "130000.00",
		variants: buildVariants(sizesXStoXL, []string{cOq, cPushti}, 5, nil, nil),
		images:   productImages(2, "d1242f", "Silk Blouse", true),
	},
	{
		slug: "women-blouse-cotton", categorySlug: "women-blouses", sku: "WBL-02",
		nameUz: "Paxta bluzka", nameRu: "Хлопковая блузка", nameEn: "Cotton blouse",
		descUz: "Kundalik kiyish uchun paxta bluzka.", descRu: "Хлопковая блузка на каждый день.", descEn: "A cotton blouse for everyday wear.",
		basePrice: "149000.00", costPrice: "75000.00",
		variants: buildVariants(sizesXStoXL, []string{cOq, cKok}, 4, nil, nil),
		images:   productImages(1, "0969da", "Cotton Blouse", false),
	},
	{
		slug: "women-blouse-office", categorySlug: "women-blouses", sku: "WBL-03",
		nameUz: "Ofis bluzkasi", nameRu: "Офисная блузка", nameEn: "Office blouse",
		descUz: "Ish kiyimi uchun qat’iy uslubdagi bluzka.", descRu: "Блузка строгого кроя для офиса.", descEn: "A tailored blouse suited to office wear.",
		basePrice: "189000.00", costPrice: "95000.00", promoPrice: "159000.00",
		variants: buildVariants(sizesXStoXL, []string{cOq, cKulrang}, 4, nil, nil),
		images:   productImages(2, "24292f", "Office Blouse", false),
	},
	{
		slug: "women-blouse-lace", categorySlug: "women-blouses", sku: "WBL-04",
		nameUz: "Chipkinli bluzka", nameRu: "Блузка с кружевом", nameEn: "Lace blouse",
		descUz: "Chipkin detallar bilan bezatilgan bluzka.", descRu: "Блузка с кружевными деталями.", descEn: "A blouse trimmed with lace detail.",
		basePrice: "219000.00", costPrice: "112000.00",
		variants: buildVariants(sizesStoL, []string{cOq, cBinafsha}, 3, nil, nil),
		images:   productImages(2, "6639ba", "Lace Blouse", true),
	},
	{
		slug: "women-blouse-oversize", categorySlug: "women-blouses", sku: "WBL-05",
		nameUz: "Keng bluzka", nameRu: "Блузка оверсайз", nameEn: "Oversized blouse",
		descUz: "Erkin fasonli, qulay oversize bluzka.", descRu: "Свободная блузка оверсайз.", descEn: "A relaxed, oversized-fit blouse.",
		basePrice: "179000.00", costPrice: "90000.00",
		variants: buildVariants(sizesStoXXL, []string{cKulrang, cYashil}, 4, nil, nil),
		images:   productImages(1, "1a7f37", "Oversized Blouse", false),
	},

	// Kids
	{
		slug: "kids-tshirt", categorySlug: "kids", sku: "KID-01",
		nameUz: "Bolalar futbolkasi", nameRu: "Детская футболка", nameEn: "Kids t-shirt",
		descUz: "Yengil paxta futbolka, kundalik kiyish uchun.", descRu: "Лёгкая хлопковая футболка на каждый день.", descEn: "A light cotton t-shirt for everyday play.",
		basePrice: "89000.00", costPrice: "40000.00", isFeatured: true,
		variants: buildVariants(kidsSizes, []string{cQizil, cKok, cSariq}, 6, nil, nil),
		images:   productImages(2, "cf222e", "Kids T-shirt", true),
	},
	{
		slug: "kids-dress", categorySlug: "kids", sku: "KID-02",
		nameUz: "Bolalar ko‘ylagi", nameRu: "Детское платье", nameEn: "Kids dress",
		descUz: "Bayramona tadbirlar uchun bolalar ko‘ylagi.", descRu: "Детское платье для праздничных случаев.", descEn: "A kids' dress for festive occasions.",
		basePrice: "129000.00", costPrice: "62000.00",
		variants: buildVariants(kidsSizes, []string{cPushti, cSariq}, 4, map[int]string{0: "139000.00"}, nil),
		images:   productImages(2, "d1242f", "Kids Dress", false),
	},
	{
		slug: "kids-jacket", categorySlug: "kids", sku: "KID-03",
		nameUz: "Bolalar kurtkasi", nameRu: "Детская куртка", nameEn: "Kids jacket",
		descUz: "Sovuq kunlar uchun issiq bolalar kurtkasi.", descRu: "Тёплая детская куртка для холодных дней.", descEn: "A warm kids' jacket for cold days.",
		basePrice: "259000.00", costPrice: "135000.00", promoPrice: "219000.00",
		variants: buildVariants(kidsSizes, []string{cKok, cYashil}, 4, nil, map[int]bool{3: true}),
		images:   productImages(2, "1a7f37", "Kids Jacket", true),
	},
	{
		slug: "kids-trousers", categorySlug: "kids", sku: "KID-04",
		nameUz: "Bolalar shimi", nameRu: "Детские брюки", nameEn: "Kids trousers",
		descUz: "Faol o‘yinlar uchun bardoshli shim.", descRu: "Прочные брюки для активных игр.", descEn: "Sturdy trousers built for active play.",
		basePrice: "109000.00", costPrice: "52000.00",
		variants: buildVariants(kidsSizes, []string{cKulrang, cKok}, 3, nil, nil),
		images:   productImages(1, "24292f", "Kids Trousers", false),
	},
	{
		slug: "kids-set", categorySlug: "kids", sku: "KID-05",
		nameUz: "Bolalar kostyumi", nameRu: "Детский костюм", nameEn: "Kids outfit set",
		descUz: "Futbolka va shimdan iborat bolalar kostyumi.", descRu: "Костюм для детей: футболка и брюки.", descEn: "A matching kids' set: t-shirt and trousers.",
		basePrice: "179000.00", costPrice: "88000.00",
		variants: buildVariants(kidsSizes, []string{cYashil, cQizil}, 4, nil, nil),
		images:   productImages(2, "1f6feb", "Kids Outfit Set", true),
	},
}
