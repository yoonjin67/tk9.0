package main

import . "modernc.org/tk9.0"

const tex = `$$\int _0 ^\infty {{\sin ax \sin bx}\over{x^2}}\,dx = {\pi a\over 2}$$`

func main() {
	Pack(
		Label(Relief("sunken"), Image(NewPhoto(Data(TeX(tex, TkScaling()*72/600))))),
		TExit(), Padx("1m"), Pady("2m"), Ipadx("1m"), Ipady("1m"),
	)
	App.Center().Wait()
}