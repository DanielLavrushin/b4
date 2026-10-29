import useBaseUrl from "@docusaurus/useBaseUrl";
import useDocusaurusContext from "@docusaurus/useDocusaurusContext";

export default function useScreenshot(path: string, absolute = false) {
  const { i18n } = useDocusaurusContext();
  const slash = path.lastIndexOf("/");
  const localized =
    i18n.currentLocale === i18n.defaultLocale
      ? path
      : `${path.slice(0, slash)}/${i18n.currentLocale}${path.slice(slash)}`;
  return useBaseUrl(localized, { absolute });
}
