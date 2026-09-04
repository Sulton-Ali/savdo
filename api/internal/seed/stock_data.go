package seed

// supplierSpec is one of the three demo suppliers Stock creates (D-49):
// realistic Uzbek clothing-wholesale businesses, one per broad category
// group the demo catalogue's purchases cycle through.
type supplierSpec struct {
	name, contactName, phone, note string
}

var supplierSpecs = []supplierSpec{
	{
		name:        "Andijon Tekstil Ulgurji",
		contactName: "Bahodir Yusupov",
		phone:       "+998901234501",
		note:        "Erkaklar kiyimlari va matolar bo‘yicha ulgurji ta’minotchi.",
	},
	{
		name:        "Marg‘ilon Ipak Karvon",
		contactName: "Nodira Karimova",
		phone:       "+998901234502",
		note:        "Ayollar kiyimlari va ipak mato yetkazib beruvchi.",
	},
	{
		name:        "Toshkent Bolalar Kiyim Servis",
		contactName: "Sardor Aliyev",
		phone:       "+998901234503",
		note:        "Bolalar kiyimlari bo‘yicha asosiy hamkor.",
	},
}
