port module Main exposing (..)

import Animation exposing (percent, px)
import Animation.Messenger
import Browser
import Html exposing (..)
import Html.Attributes exposing (..)
import Html.Events exposing (..)
import Json.Decode as D
import Time



-- MAIN


main : Program Float Model Msg
main =
    Browser.element
        { init = init
        , update = update
        , subscriptions = subscriptions
        , view = view
        }



-- PORTS


port playUrl : String -> Cmd msg


port messageReceiver : (String -> msg) -> Sub msg



-- MODEL


type alias WebsocketMessage =
    { client_id : String
    , audio_url : String
    , text : String
    , username : String
    , user_picture : String
    }


type alias Card =
    { username : String
    , text : String
    , user_picture : String
    , animStyle : Animation.Messenger.State Msg
    }


type alias Model =
    { cards : List Card
    , innerHeight : Float
    }


init : Float -> ( Model, Cmd Msg )
init innerHeight =
    ( Model [] innerHeight, Cmd.none )



-- UPDATE


type Msg
    = Recv String
    | Animate Animation.Msg
    | Finished


update : Msg -> Model -> ( Model, Cmd Msg )
update msg model =
    case msg of
        Animate animMsg ->
            let
                stylesAndCmds =
                    List.map (\card -> Animation.Messenger.update animMsg card.animStyle) model.cards

                styles =
                    List.map Tuple.first stylesAndCmds

                cmds =
                    List.map Tuple.second stylesAndCmds

                updateStyle card style =
                    { card | animStyle = style }
            in
            ( { model | cards = List.map2 updateStyle model.cards styles }
            , Cmd.batch cmds
            )

        Recv json ->
            case D.decodeString websocketMessageDecoder json of
                Ok ws ->
                    let
                        newAnimation =
                            Animation.interrupt
                                [ Animation.to [ Animation.translate (percent 0) (px 0) ]
                                , Animation.wait (Time.millisToPosix <| 15 * 1000)
                                , Animation.to [ Animation.translate (percent 115) (percent 0) ]
                                , Animation.Messenger.send Finished
                                ]
                                (Animation.style [ Animation.translate (percent 0) (px model.innerHeight) ])

                        newCard =
                            Card ws.username ws.text ws.user_picture newAnimation
                    in
                    ( { model | cards = model.cards ++ [ newCard ] }
                    , playUrl ws.audio_url
                    )

                Err _ ->
                    ( model, Cmd.none )

        Finished ->
            ( { model | cards = List.drop 1 model.cards }, Cmd.none )



-- SUBSCRIPTIONS


subscriptions : Model -> Sub Msg
subscriptions model =
    Sub.batch
        [ messageReceiver Recv
        , Animation.subscription Animate <| List.map .animStyle model.cards
        ]



-- VIEW


cardView : Card -> Html Msg
cardView card =
    div
        (Animation.render card.animStyle
            ++ [ class "content" ]
        )
        [ div [ class "user-picture" ] [ img [ src card.user_picture ] [] ]
        , div [ class "container" ]
            [ div [ class "username" ] [ text <| card.username ++ " disse:" ]
            , div [ class "text" ] [ text card.text ]
            ]
        ]


view : Model -> Html Msg
view model =
    div [ class "main" ] (List.map cardView model.cards)



-- JSON decode


websocketMessageDecoder : D.Decoder WebsocketMessage
websocketMessageDecoder =
    D.map5 WebsocketMessage
        (D.field "client_id" D.string)
        (D.field "audio_url" D.string)
        (D.field "text" D.string)
        (D.field "username" D.string)
        (D.field "user_picture" D.string)
