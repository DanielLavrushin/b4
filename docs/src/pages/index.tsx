import "@fontsource-variable/geist";
import "@fontsource-variable/geist-mono";
import Head from "@docusaurus/Head";
import { translate } from "@docusaurus/Translate";
import Layout from "@theme/Layout";
import DpiSection from "@site/src/components/Landing/DpiSection";
import Faq from "@site/src/components/Landing/Faq";
import Features from "@site/src/components/Landing/Features";
import FinalCta from "@site/src/components/Landing/FinalCta";
import Hero from "@site/src/components/Landing/Hero";
import Platforms from "@site/src/components/Landing/Platforms";
import Steps from "@site/src/components/Landing/Steps";
import Tagline from "@site/src/components/Landing/Tagline";
import shared from "@site/src/components/Landing/landing.module.css";
import useScreenshot from "@site/src/components/Landing/useScreenshot";

export default function Home() {
  const socialImage = useScreenshot("/img/dashboard/20261003220000.png", true);

  return (
    <Layout
      title={translate({
        id: "home.meta.title",
        message: "DPI bypass and traffic routing for Linux routers",
      })}
      description={translate({
        id: "home.meta.description",
        message:
          "b4 is a Linux router service that changes how the packets of selected sites look to the provider's DPI, or sends them through a VPN or a proxy.",
      })}
    >
      <Head>
        <meta property="og:image" content={socialImage} />
        <meta name="twitter:image" content={socialImage} />
      </Head>
      <main className={shared.root}>
        <Hero />
        <DpiSection />
        <Features />
        <Tagline />
        <Steps />
        <Platforms />
        <Faq />
        <FinalCta />
      </main>
    </Layout>
  );
}
