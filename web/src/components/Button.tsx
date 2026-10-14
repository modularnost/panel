import type { ButtonHTMLAttributes } from "react";
import { Link, type LinkProps } from "react-router";

export type Variant = "filled" | "tonal" | "outlined" | "text" | "danger" | "icon";

const cls = (variant: Variant) => `btn ${variant}`;

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant };

export function Button({ variant = "filled", ...rest }: ButtonProps) {
  return <button className={cls(variant)} {...rest} />;
}

export function LinkButton({ variant = "filled", ...rest }: LinkProps & { variant?: Variant }) {
  return <Link className={cls(variant)} {...rest} />;
}
