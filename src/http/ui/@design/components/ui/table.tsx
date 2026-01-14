import * as React from "react";

import { cn } from "@design/lib/utils";

function Table({ className, ...props }: React.ComponentProps<"table">) {
  return (
    <div data-slot="table-container" className="relative w-full">
      <table
        data-slot="table"
        className={cn(
          "w-full caption-bottom border-collapse text-xs",
          className,
        )}
        {...props}
      />
    </div>
  );
}

function TableHeader({ className, ...props }: React.ComponentProps<"thead">) {
  return (
    <thead
      data-slot="table-header"
      className={cn(
        "bg-background sticky top-0 z-10 [&_tr]:border-b",
        className,
      )}
      {...props}
    />
  );
}

function TableBody({ className, ...props }: React.ComponentProps<"tbody">) {
  return (
    <tbody
      data-slot="table-body"
      className={cn("[&_tr:last-child]:border-0", className)}
      {...props}
    />
  );
}

function TableFooter({ className, ...props }: React.ComponentProps<"tfoot">) {
  return (
    <tfoot
      data-slot="table-footer"
      className={cn(
        "bg-muted/50 border-t font-medium [&>tr]:last:border-b-0",
        className,
      )}
      {...props}
    />
  );
}

function TableRow({ className, ...props }: React.ComponentProps<"tr">) {
  return (
    <tr
      data-slot="table-row"
      className={cn(
        "hover:bg-muted/50 data-[state=selected]:bg-muted h-10.25 border-b transition-colors",
        className,
      )}
      {...props}
    />
  );
}

function TableHead({ className, ...props }: React.ComponentProps<"th">) {
  return (
    <th
      data-slot="table-head"
      className={cn(
        "text-foreground h-10 px-2 text-left align-middle font-medium whitespace-nowrap [&:has([role=checkbox])]:pr-0",
        className,
      )}
      {...props}
    />
  );
}

interface TableCellProps extends React.ComponentProps<"td"> {
  variant?: "default" | "mono";
}

function TableCell({
  className,
  variant = "default",
  onClick,
  ...props
}: TableCellProps) {
  return (
    <td
      data-slot="table-cell"
      className={cn(
        "p-2 align-middle [&:has([role=checkbox])]:pr-0",
        "[&>div]:flex [&>div]:items-center [&>div]:gap-2",
        "[&_svg]:text-muted-foreground [&_svg]:hover:text-foreground [&_svg]:h-4 [&_svg]:w-4 [&_svg]:cursor-pointer [&_svg]:transition-colors",
        "[&>div>span]:hover:bg-accent [&>div>span]:hover:text-accent-foreground [&>div>span]:cursor-pointer",
        onClick &&
          "hover:bg-accent hover:text-accent-foreground cursor-pointer",
        variant === "mono" && "text-muted-foreground font-mono text-xs",
        className,
      )}
      onClick={onClick}
      {...props}
    />
  );
}

function TableCaption({
  className,
  ...props
}: React.ComponentProps<"caption">) {
  return (
    <caption
      data-slot="table-caption"
      className={cn("text-muted-foreground mt-4 text-xs", className)}
      {...props}
    />
  );
}

export {
  Table,
  TableHeader,
  TableBody,
  TableFooter,
  TableHead,
  TableRow,
  TableCell,
  TableCaption,
};
